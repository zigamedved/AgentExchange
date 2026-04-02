package platform

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteAuth is a persistent Auth implementation backed by SQLite.
type SQLiteAuth struct {
	db             *sql.DB
	mu             sync.RWMutex
	defaultCredits float64
}

// NewSQLiteAuth opens (or creates) a SQLite database for org/auth storage.
// Uses the same DB file as the registry if you pass the same path.
func NewSQLiteAuth(path string, defaultCredits float64) (*SQLiteAuth, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateAuth(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate auth: %w", err)
	}
	return &SQLiteAuth{db: db, defaultCredits: defaultCredits}, nil
}

func migrateAuth(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS orgs (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			api_key     TEXT NOT NULL UNIQUE,
			visibility  TEXT NOT NULL DEFAULT 'private',
			credits     REAL NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL
		)
	`)
	return err
}

// Seed adds a pre-configured org (for demo/testing).
func (a *SQLiteAuth) Seed(id, name, key string, vis OrgVisibility, credits float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, _ = a.db.Exec(`
		INSERT INTO orgs (id, name, api_key, visibility, credits, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			api_key = excluded.api_key,
			visibility = excluded.visibility,
			credits = excluded.credits
	`, id, name, key, string(vis), credits, time.Now().Format(time.RFC3339))
}

func (a *SQLiteAuth) Authenticate(apiKey string) *Org {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.scanOrg("SELECT id, name, api_key, visibility, credits, created_at FROM orgs WHERE api_key = ?", apiKey)
}

func (a *SQLiteAuth) Register(name string, visibility OrgVisibility) (*Org, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	key, err := generateAPIKey()
	if err != nil {
		return nil, err
	}
	id := "org-" + generateShortID()
	now := time.Now()

	a.mu.Lock()
	defer a.mu.Unlock()
	_, err = a.db.Exec(`
		INSERT INTO orgs (id, name, api_key, visibility, credits, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, name, key, string(visibility), a.defaultCredits, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("insert org: %w", err)
	}
	return &Org{
		ID:         id,
		Name:       name,
		APIKey:     key,
		Visibility: visibility,
		Credits:    a.defaultCredits,
		CreatedAt:  now,
	}, nil
}

func (a *SQLiteAuth) DeductCredits(apiKey string, amount float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var credits float64
	err := a.db.QueryRow("SELECT credits FROM orgs WHERE api_key = ?", apiKey).Scan(&credits)
	if err != nil {
		return fmt.Errorf("org not found")
	}
	if credits < amount {
		return fmt.Errorf("insufficient credits: have %.4f, need %.4f", credits, amount)
	}
	_, err = a.db.Exec("UPDATE orgs SET credits = credits - ? WHERE api_key = ?", amount, apiKey)
	return err
}

func (a *SQLiteAuth) AddCredits(apiKey string, amount float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	result, err := a.db.Exec("UPDATE orgs SET credits = credits + ? WHERE api_key = ?", amount, apiKey)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("org not found")
	}
	return nil
}

func (a *SQLiteAuth) GetByID(id string) *Org {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.scanOrg("SELECT id, name, api_key, visibility, credits, created_at FROM orgs WHERE id = ?", id)
}

func (a *SQLiteAuth) All() []*Org {
	a.mu.RLock()
	defer a.mu.RUnlock()

	rows, err := a.db.Query("SELECT id, name, api_key, visibility, credits, created_at FROM orgs")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*Org
	for rows.Next() {
		if o := a.scanOrgRow(rows); o != nil {
			out = append(out, o)
		}
	}
	return out
}

// Close closes the database connection.
func (a *SQLiteAuth) Close() error {
	return a.db.Close()
}

func (a *SQLiteAuth) scanOrg(query string, args ...any) *Org {
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	if !rows.Next() {
		return nil
	}
	return a.scanOrgRow(rows)
}

func (a *SQLiteAuth) scanOrgRow(rows *sql.Rows) *Org {
	var (
		o         Org
		vis       string
		createdAt string
	)
	if err := rows.Scan(&o.ID, &o.Name, &o.APIKey, &vis, &o.Credits, &createdAt); err != nil {
		return nil
	}
	o.Visibility = OrgVisibility(vis)
	o.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &o
}

// ─── SQLiteInviteStore ──────────────────────────────────────────────────────

// SQLiteInviteStore is a persistent InviteStore backed by SQLite.
type SQLiteInviteStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteInviteStore opens (or creates) invite tables in the given SQLite database.
func NewSQLiteInviteStore(path string) (*SQLiteInviteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS invites (
			code        TEXT PRIMARY KEY,
			created_by  TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			redeemed_by TEXT NOT NULL DEFAULT '',
			redeemed_at TEXT NOT NULL DEFAULT '',
			used        INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate invites: %w", err)
	}
	return &SQLiteInviteStore{db: db}, nil
}

func (s *SQLiteInviteStore) Create(createdBy string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := "inv_" + hex.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO invites (code, created_by, created_at) VALUES (?, ?, ?)",
		code, createdBy, time.Now().Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	return code, nil
}

func (s *SQLiteInviteStore) Validate(code string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var used int
	err := s.db.QueryRow("SELECT used FROM invites WHERE code = ?", code).Scan(&used)
	if err != nil {
		return fmt.Errorf("invalid invite code")
	}
	if used != 0 {
		return fmt.Errorf("invite already redeemed")
	}
	return nil
}

func (s *SQLiteInviteStore) Redeem(code string, orgID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(
		"UPDATE invites SET used = 1, redeemed_by = ?, redeemed_at = ? WHERE code = ? AND used = 0",
		orgID, time.Now().Format(time.RFC3339), code,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("invite already redeemed or not found")
	}
	return nil
}

func (s *SQLiteInviteStore) List() []*Invite {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT code, created_by, created_at, redeemed_by, redeemed_at, used FROM invites")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*Invite
	for rows.Next() {
		var (
			inv        Invite
			createdAt  string
			redeemedAt string
			used       int
		)
		if err := rows.Scan(&inv.Code, &inv.CreatedBy, &createdAt, &inv.RedeemedBy, &redeemedAt, &used); err != nil {
			continue
		}
		inv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if redeemedAt != "" {
			inv.RedeemedAt, _ = time.Parse(time.RFC3339, redeemedAt)
		}
		inv.Used = used != 0
		out = append(out, &inv)
	}
	return out
}

// Close closes the database connection.
func (s *SQLiteInviteStore) Close() error {
	return s.db.Close()
}
