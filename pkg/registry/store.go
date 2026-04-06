// Package registry implements the AgentExchange agent registry.
// It defines the Store interface for pluggable backends and ships
// a default in-memory implementation (MemoryStore).
package registry

import (
	"github.com/zigamedved/agent-exchange/pkg/protocol"
)

// Store is the interface that registry backends must implement.
// The default implementation is MemoryStore (in-memory with TTL reaping).
// Production deployments can swap in a PostgreSQL or SQLite-backed store.
type Store interface {
	// Register adds or updates an agent. Returns the assigned agent ID.
	Register(req RegisterRequest) (string, error)

	// Deregister removes an agent by ID.
	Deregister(id string)

	// Heartbeat refreshes an agent's TTL expiry.
	Heartbeat(id string) error

	// Get returns an agent by ID.
	Get(id string) (*Entry, bool)

	// GetByName returns an agent by organization and name.
	GetByName(org, name string) (*Entry, bool)

	// Search returns all agents matching the filter.
	Search(f SearchFilter) []*Entry

	// All returns all registered agents.
	All() []*Entry

	// Close shuts down the store (stops background goroutines, closes connections).
	Close() error
}

// AgentVisibility controls who can discover and call an agent.
type AgentVisibility string

const (
	AgentPublic  AgentVisibility = "public"
	AgentPrivate AgentVisibility = "private"
)

// Entry is a registered agent in the registry.
type Entry struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Organization string             `json:"organization"`
	EndpointURL  string             `json:"endpoint_url"`
	Visibility   AgentVisibility    `json:"visibility"`
	AgentCard    protocol.AgentCard `json:"agent_card"`
	RegisteredAt string             `json:"registered_at"`
	LastSeen     string             `json:"last_seen"`
	ExpiresAt    string             `json:"expires_at"`
}

// RegisterRequest is the payload for registering an agent.
type RegisterRequest struct {
	Name         string             `json:"name"`
	Organization string             `json:"organization"`
	EndpointURL  string             `json:"endpoint_url"`
	Visibility   AgentVisibility    `json:"visibility,omitempty"` // default "public"
	AgentCard    protocol.AgentCard `json:"agent_card"`
	TTLSeconds   int                `json:"ttl_seconds,omitempty"` // default 300
}

// SearchFilter defines search criteria for registry queries.
type SearchFilter struct {
	// Skill filters agents that have a skill with this exact ID or tag.
	Skill string
	// Organization filters agents belonging to this org.
	Organization string
	// Name filters agents whose name contains this string (case-insensitive).
	Name string
	// CallerOrg is the org ID of the caller. Private agents are only returned
	// if CallerOrg matches the agent's organization.
	CallerOrg string
}
