// Package registry implements the FAXP agent registry — the in-process store
// that maps agent names to their endpoint URLs and Agent Cards.
// The platform wraps this with HTTP endpoints and persistence.
package registry

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zigamedved/faxp/pkg/protocol"
)

// Entry is a registered agent in the registry.
type Entry struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Organization string             `json:"organization"`
	EndpointURL  string             `json:"endpoint_url"`
	AgentCard    protocol.AgentCard `json:"agent_card"`
	RegisteredAt time.Time          `json:"registered_at"`
	LastSeen     time.Time          `json:"last_seen"`
	TTL          time.Duration      `json:"-"`
	ExpiresAt    time.Time          `json:"expires_at"`
}

// RegisterRequest is the payload for registering an agent.
type RegisterRequest struct {
	Name         string             `json:"name"`
	Organization string             `json:"organization"`
	EndpointURL  string             `json:"endpoint_url"`
	AgentCard    protocol.AgentCard `json:"agent_card"`
	TTLSeconds   int                `json:"ttl_seconds,omitempty"` // default 300
}

// Registry is a thread-safe in-memory store of FAXP agent entries.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*Entry // key: agent ID
	byName  map[string]string // key: "org/name" → agent ID
}

// New returns an empty Registry.
func New() *Registry {
	r := &Registry{
		entries: make(map[string]*Entry),
		byName:  make(map[string]string),
	}
	go r.reap()
	return r
}

// Register adds or updates an agent. Returns the assigned agent ID.
func (r *Registry) Register(req RegisterRequest) (string, error) {
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

	r.mu.Lock()
	defer r.mu.Unlock()

	nameKey := nameKey(req.Organization, req.Name)

	// Re-registration: reuse existing ID
	id, exists := r.byName[nameKey]
	if !exists {
		id = uuid.New().String()
		r.byName[nameKey] = id
	}

	now := time.Now()
	r.entries[id] = &Entry{
		ID:           id,
		Name:         req.Name,
		Organization: req.Organization,
		EndpointURL:  req.EndpointURL,
		AgentCard:    req.AgentCard,
		RegisteredAt: now,
		LastSeen:     now,
		TTL:          ttl,
		ExpiresAt:    now.Add(ttl),
	}
	return id, nil
}

// Deregister removes an agent by ID.
func (r *Registry) Deregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		delete(r.byName, nameKey(e.Organization, e.Name))
		delete(r.entries, id)
	}
}

// Heartbeat refreshes an agent's TTL expiry.
func (r *Registry) Heartbeat(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	now := time.Now()
	e.LastSeen = now
	e.ExpiresAt = now.Add(e.TTL)
	return nil
}

// Get returns an agent by ID.
func (r *Registry) Get(id string) (*Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	return e, ok
}

// GetByName returns an agent by organization and name.
func (r *Registry) GetByName(org, name string) (*Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byName[nameKey(org, name)]
	if !ok {
		return nil, false
	}
	e, ok := r.entries[id]
	return e, ok
}

// SearchFilter defines search criteria for registry queries.
type SearchFilter struct {
	// Skill filters agents that have a skill with this exact ID or tag.
	Skill string
	// Organization filters agents belonging to this org.
	Organization string
	// Name filters agents whose name contains this string (case-insensitive).
	Name string
}

// Search returns all agents matching the filter.
func (r *Registry) Search(f SearchFilter) []*Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*Entry
	for _, e := range r.entries {
		if !r.matchesFilter(e, f) {
			continue
		}
		results = append(results, e)
	}
	return results
}

// All returns all registered agents.
func (r *Registry) All() []*Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	return out
}

func (r *Registry) matchesFilter(e *Entry, f SearchFilter) bool {
	if f.Organization != "" && !strings.EqualFold(e.Organization, f.Organization) {
		return false
	}
	if f.Name != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(f.Name)) {
		return false
	}
	if f.Skill != "" {
		found := false
		for _, s := range e.AgentCard.Skills {
			if strings.EqualFold(s.ID, f.Skill) {
				found = true
				break
			}
			for _, tag := range s.Tags {
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
func (r *Registry) reap() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for id, e := range r.entries {
			if now.After(e.ExpiresAt) {
				delete(r.byName, nameKey(e.Organization, e.Name))
				delete(r.entries, id)
			}
		}
		r.mu.Unlock()
	}
}

func nameKey(org, name string) string {
	return strings.ToLower(org) + "/" + strings.ToLower(name)
}
