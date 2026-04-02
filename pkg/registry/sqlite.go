package registry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteStore is a persistent registry backed by SQLite.
type SQLiteStore struct {
	db   *sql.DB
	mu   sync.RWMutex
	done chan struct{}
}

// NewSQLiteStore opens (or creates) a SQLite database at the given path.
// Use ":memory:" for an in-memory database, or a file path for persistence.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for concurrent reads
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	s := &SQLiteStore{db: db, done: make(chan struct{})}
	go s.reap()
	return s, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id           TEXT PRIMARY KEY,
			name         TEXT NOT NULL,
			organization TEXT NOT NULL,
			endpoint_url TEXT NOT NULL,
			visibility   TEXT NOT NULL DEFAULT 'public',
			agent_card   TEXT NOT NULL,
			ttl_seconds  INTEGER NOT NULL DEFAULT 300,
			registered_at TEXT NOT NULL,
			last_seen     TEXT NOT NULL,
			expires_at    TEXT NOT NULL,
			UNIQUE(organization, name)
		)
	`)
	return err
}

func (s *SQLiteStore) Register(req RegisterRequest) (string, error) {
	if req.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if req.EndpointURL == "" {
		return "", fmt.Errorf("endpoint_url is required")
	}

	ttl := req.TTLSeconds
	if ttl <= 0 {
		ttl = 300
	}

	vis := req.Visibility
	if vis == "" {
		vis = AgentPublic
	}

	cardJSON, err := json.Marshal(req.AgentCard)
	if err != nil {
		return "", fmt.Errorf("marshal agent card: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(ttl) * time.Second)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for existing registration (upsert)
	var id string
	err = s.db.QueryRow(
		"SELECT id FROM agents WHERE organization = ? AND name = ?",
		req.Organization, req.Name,
	).Scan(&id)

	if err == sql.ErrNoRows {
		id = uuid.New().String()
	} else if err != nil {
		return "", fmt.Errorf("lookup: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO agents (id, name, organization, endpoint_url, visibility, agent_card, ttl_seconds, registered_at, last_seen, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(organization, name) DO UPDATE SET
			endpoint_url = excluded.endpoint_url,
			visibility = excluded.visibility,
			agent_card = excluded.agent_card,
			ttl_seconds = excluded.ttl_seconds,
			last_seen = excluded.last_seen,
			expires_at = excluded.expires_at
	`, id, req.Name, req.Organization, req.EndpointURL, string(vis), string(cardJSON),
		ttl, now.Format(time.RFC3339), now.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
	if err != nil {
		return "", fmt.Errorf("upsert: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) Deregister(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec("DELETE FROM agents WHERE id = ?", id)
}

func (s *SQLiteStore) Heartbeat(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ttlSeconds int
	err := s.db.QueryRow("SELECT ttl_seconds FROM agents WHERE id = ?", id).Scan(&ttlSeconds)
	if err != nil {
		return fmt.Errorf("agent %s not found", id)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)
	_, err = s.db.Exec(
		"UPDATE agents SET last_seen = ?, expires_at = ? WHERE id = ?",
		now.Format(time.RFC3339), expiresAt.Format(time.RFC3339), id,
	)
	return err
}

func (s *SQLiteStore) Get(id string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanOne("SELECT id, name, organization, endpoint_url, visibility, agent_card, registered_at, last_seen, expires_at FROM agents WHERE id = ?", id)
}

func (s *SQLiteStore) GetByName(org, name string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanOne("SELECT id, name, organization, endpoint_url, visibility, agent_card, registered_at, last_seen, expires_at FROM agents WHERE organization = ? AND name = ?", org, name)
}

func (s *SQLiteStore) Search(f SearchFilter) []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, name, organization, endpoint_url, visibility, agent_card, registered_at, last_seen, expires_at FROM agents WHERE 1=1"
	var args []any

	if f.Organization != "" {
		query += " AND LOWER(organization) = LOWER(?)"
		args = append(args, f.Organization)
	}
	if f.Name != "" {
		query += " AND LOWER(name) LIKE LOWER(?)"
		args = append(args, "%"+f.Name+"%")
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []*Entry
	for rows.Next() {
		e, ok := s.scanEntry(rows)
		if !ok {
			continue
		}
		// Apply visibility filter
		if e.Visibility == AgentPrivate && f.CallerOrg != "" && !strings.EqualFold(e.Organization, f.CallerOrg) {
			continue
		}
		// Apply skill filter (need to check in Go since skills are in JSON)
		if f.Skill != "" && !entryMatchesSkill(e, f.Skill) {
			continue
		}
		results = append(results, e)
	}
	return results
}

func (s *SQLiteStore) All() []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT id, name, organization, endpoint_url, visibility, agent_card, registered_at, last_seen, expires_at FROM agents")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []*Entry
	for rows.Next() {
		e, ok := s.scanEntry(rows)
		if ok {
			results = append(results, e)
		}
	}
	return results
}

func (s *SQLiteStore) Close() error {
	close(s.done)
	return s.db.Close()
}

// reap removes expired entries every 30 seconds.
func (s *SQLiteStore) reap() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			_, _ = s.db.Exec("DELETE FROM agents WHERE expires_at < ?", time.Now().Format(time.RFC3339))
			s.mu.Unlock()
		}
	}
}

// ─── Scan helpers ───────────────────────────────────────────────────────────

func (s *SQLiteStore) scanOne(query string, args ...any) (*Entry, bool) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false
	}
	return s.scanEntry(rows)
}

func (s *SQLiteStore) scanEntry(rows *sql.Rows) (*Entry, bool) {
	var (
		e       Entry
		vis     string
		cardStr string
	)
	if err := rows.Scan(&e.ID, &e.Name, &e.Organization, &e.EndpointURL, &vis, &cardStr, &e.RegisteredAt, &e.LastSeen, &e.ExpiresAt); err != nil {
		return nil, false
	}
	e.Visibility = AgentVisibility(vis)
	if err := json.Unmarshal([]byte(cardStr), &e.AgentCard); err != nil {
		return nil, false
	}
	return &e, true
}

func entryMatchesSkill(e *Entry, skill string) bool {
	for _, sk := range e.AgentCard.Skills {
		if strings.EqualFold(sk.ID, skill) {
			return true
		}
		for _, tag := range sk.Tags {
			if strings.EqualFold(tag, skill) {
				return true
			}
		}
	}
	return false
}
