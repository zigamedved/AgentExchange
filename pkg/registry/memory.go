package registry

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// memoryEntry extends Entry with parsed time fields for TTL tracking.
type memoryEntry struct {
	Entry
	registeredAt time.Time
	lastSeen     time.Time
	expiresAt    time.Time
	ttl          time.Duration
}

// MemoryStore is a thread-safe in-memory registry with TTL-based expiration.
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]*memoryEntry // key: agent ID
	byName  map[string]string      // key: "org/name" → agent ID
	done    chan struct{}
}

// NewMemoryStore returns an empty in-memory Store with background TTL reaping.
func NewMemoryStore() *MemoryStore {
	s := &MemoryStore{
		entries: make(map[string]*memoryEntry),
		byName:  make(map[string]string),
		done:    make(chan struct{}),
	}
	go s.reap()
	return s
}

func (s *MemoryStore) Register(req RegisterRequest) (string, error) {
	if req.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if req.EndpointURL == "" {
		return "", fmt.Errorf("endpoint_url is required")
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	nk := nameKey(req.Organization, req.Name)

	// Re-registration: reuse existing ID
	id, exists := s.byName[nk]
	if !exists {
		id = uuid.New().String()
		s.byName[nk] = id
	}

	now := time.Now()
	s.entries[id] = &memoryEntry{
		Entry: Entry{
			ID:           id,
			Name:         req.Name,
			Organization: req.Organization,
			EndpointURL:  req.EndpointURL,
			AgentCard:    req.AgentCard,
			RegisteredAt: now.Format(time.RFC3339),
			LastSeen:     now.Format(time.RFC3339),
			ExpiresAt:    now.Add(ttl).Format(time.RFC3339),
		},
		registeredAt: now,
		lastSeen:     now,
		expiresAt:    now.Add(ttl),
		ttl:          ttl,
	}
	return id, nil
}

func (s *MemoryStore) Deregister(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[id]; ok {
		delete(s.byName, nameKey(e.Organization, e.Name))
		delete(s.entries, id)
	}
}

func (s *MemoryStore) Heartbeat(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	now := time.Now()
	e.lastSeen = now
	e.expiresAt = now.Add(e.ttl)
	e.LastSeen = now.Format(time.RFC3339)
	e.ExpiresAt = e.expiresAt.Format(time.RFC3339)
	return nil
}

func (s *MemoryStore) Get(id string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok {
		return nil, false
	}
	return &e.Entry, true
}

func (s *MemoryStore) GetByName(org, name string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byName[nameKey(org, name)]
	if !ok {
		return nil, false
	}
	e, ok := s.entries[id]
	if !ok {
		return nil, false
	}
	return &e.Entry, true
}

func (s *MemoryStore) Search(f SearchFilter) []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*Entry
	for _, e := range s.entries {
		if !matchesFilter(&e.Entry, f) {
			continue
		}
		results = append(results, &e.Entry)
	}
	return results
}

func (s *MemoryStore) All() []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, &e.Entry)
	}
	return out
}

func (s *MemoryStore) Close() error {
	close(s.done)
	return nil
}

func matchesFilter(e *Entry, f SearchFilter) bool {
	if f.Organization != "" && !strings.EqualFold(e.Organization, f.Organization) {
		return false
	}
	if f.Name != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(f.Name)) {
		return false
	}
	if f.Skill != "" {
		found := false
		for _, sk := range e.AgentCard.Skills {
			if strings.EqualFold(sk.ID, f.Skill) {
				found = true
				break
			}
			for _, tag := range sk.Tags {
				if strings.EqualFold(tag, f.Skill) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// reap removes expired entries every 30 seconds.
func (s *MemoryStore) reap() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for id, e := range s.entries {
				if now.After(e.expiresAt) {
					delete(s.byName, nameKey(e.Organization, e.Name))
					delete(s.entries, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func nameKey(org, name string) string {
	return strings.ToLower(org) + "/" + strings.ToLower(name)
}
