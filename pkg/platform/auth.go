package platform

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// OrgVisibility controls whether an org's agents are discoverable publicly.
type OrgVisibility string

const (
	OrgPublic  OrgVisibility = "public"
	OrgPrivate OrgVisibility = "private"
)

// RegistrationMode controls how new orgs are created.
type RegistrationMode string

const (
	RegistrationOpen   RegistrationMode = "open"   // anyone can create orgs
	RegistrationInvite RegistrationMode = "invite" // need an invite code
	RegistrationClosed RegistrationMode = "closed" // admin only
)

// Org represents an organization registered on the platform.
type Org struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	APIKey     string        `json:"api_key"`
	Visibility OrgVisibility `json:"visibility"`
	Credits    float64       `json:"credits"`
	CreatedAt  time.Time     `json:"created_at"`
}

// Auth is the interface that platform authentication backends must implement.
// The default implementation is MemoryAuth (in-memory with generated API keys).
// Production deployments can swap in a database-backed implementation.
type Auth interface {
	// Authenticate returns the Org for a given API key, or nil if not found.
	Authenticate(apiKey string) *Org

	// Register creates a new org with a generated API key. Returns the new org.
	Register(name string, visibility OrgVisibility) (*Org, error)

	// GetByID returns an org by its ID.
	GetByID(id string) *Org

	// All returns all registered organizations.
	All() []*Org

	// DeductCredits subtracts amount from the org's balance.
	DeductCredits(apiKey string, amount float64) error

	// AddCredits adds amount to the org's balance.
	AddCredits(apiKey string, amount float64) error
}

// ─── MemoryAuth ─────────────────────────────────────────────────────────────

// MemoryAuth is a thread-safe in-memory Auth implementation.
type MemoryAuth struct {
	mu             sync.RWMutex
	orgs           map[string]*Org // key: api key
	defaultCredits float64
}

// NewMemoryAuth creates an empty in-memory Auth store.
func NewMemoryAuth(defaultCredits float64) *MemoryAuth {
	return &MemoryAuth{
		orgs:           make(map[string]*Org),
		defaultCredits: defaultCredits,
	}
}

// Seed adds a pre-configured org (for demo/testing). Not part of the Auth interface.
func (a *MemoryAuth) Seed(id, name, key string, vis OrgVisibility, credits float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orgs[key] = &Org{
		ID:         id,
		Name:       name,
		APIKey:     key,
		Visibility: vis,
		Credits:    credits,
		CreatedAt:  time.Now(),
	}
}

func (a *MemoryAuth) Authenticate(apiKey string) *Org {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.orgs[apiKey]
}

func (a *MemoryAuth) Register(name string, visibility OrgVisibility) (*Org, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	key, err := generateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	id := "org-" + generateShortID()
	org := &Org{
		ID:         id,
		Name:       name,
		APIKey:     key,
		Visibility: visibility,
		Credits:    a.defaultCredits,
		CreatedAt:  time.Now(),
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.orgs[key] = org
	return org, nil
}

func (a *MemoryAuth) DeductCredits(apiKey string, amount float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	org, ok := a.orgs[apiKey]
	if !ok {
		return fmt.Errorf("org not found")
	}
	if org.Credits < amount {
		return fmt.Errorf("insufficient credits: have %.4f, need %.4f", org.Credits, amount)
	}
	org.Credits -= amount
	return nil
}

func (a *MemoryAuth) AddCredits(apiKey string, amount float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	org, ok := a.orgs[apiKey]
	if !ok {
		return fmt.Errorf("org not found")
	}
	org.Credits += amount
	return nil
}

func (a *MemoryAuth) GetByID(id string) *Org {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, o := range a.orgs {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func (a *MemoryAuth) All() []*Org {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Org, 0, len(a.orgs))
	for _, o := range a.orgs {
		out = append(out, o)
	}
	return out
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func generateAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ax_" + hex.EncodeToString(b), nil
}

func generateShortID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
