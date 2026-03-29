package platform

import (
	"sync"
	"time"
)

// Org represents an organization registered on the platform.
type Org struct {
	ID        string
	Name      string
	APIKey    string
	CreatedAt time.Time
}

// AuthStore manages API keys and organizations.
type AuthStore struct {
	mu   sync.RWMutex
	orgs map[string]*Org // key: api key
}

// NewAuthStore creates an AuthStore pre-seeded with demo API keys.
func NewAuthStore() *AuthStore {
	a := &AuthStore{orgs: make(map[string]*Org)}

	// Demo organizations — hard-coded for local development
	a.seed("org-a", "Company A (Researcher)", "ax_companya_demo")
	a.seed("org-b", "Company B (Writer)", "ax_companyb_demo")
	a.seed("org-c", "Company C (Analyst)", "ax_companyc_demo")
	a.seed("org-admin", "Platform Admin", "ax_admin_demo")

	// cli client seed
	a.seed("cli-client", "CLI Client", "api-key")

	return a
}

func (a *AuthStore) seed(id, name, key string) {
	a.orgs[key] = &Org{
		ID:        id,
		Name:      name,
		APIKey:    key,
		CreatedAt: time.Now(),
	}
}

// Authenticate returns the Org associated with the given API key, or nil if not found.
func (a *AuthStore) Authenticate(apiKey string) *Org {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.orgs[apiKey]
}

// Register adds a new organization with the given API key.
func (a *AuthStore) Register(id, name, key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orgs[key] = &Org{
		ID:        id,
		Name:      name,
		APIKey:    key,
		CreatedAt: time.Now(),
	}
}

// All returns all registered organizations.
func (a *AuthStore) All() []*Org {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Org, 0, len(a.orgs))
	for _, o := range a.orgs {
		out = append(out, o)
	}
	return out
}
