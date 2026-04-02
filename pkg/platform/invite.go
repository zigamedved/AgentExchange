package platform

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// InviteStore manages invite codes for gated registration.
type InviteStore interface {
	// Create generates a new invite code. Returns the code.
	Create(createdBy string) (string, error)

	// Validate checks if an invite code is valid and unused.
	Validate(code string) error

	// Redeem marks an invite as used. Returns an error if already redeemed or invalid.
	Redeem(code string, orgID string) error

	// List returns all invites (for admin).
	List() []*Invite
}

// Invite represents a single-use registration invite.
type Invite struct {
	Code       string    `json:"code"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	RedeemedBy string    `json:"redeemed_by,omitempty"`
	RedeemedAt time.Time `json:"redeemed_at,omitempty"`
	Used       bool      `json:"used"`
}

// MemoryInviteStore is a thread-safe in-memory InviteStore.
type MemoryInviteStore struct {
	mu      sync.RWMutex
	invites map[string]*Invite
}

// NewMemoryInviteStore creates an empty in-memory invite store.
func NewMemoryInviteStore() *MemoryInviteStore {
	return &MemoryInviteStore{invites: make(map[string]*Invite)}
}

func (s *MemoryInviteStore) Create(createdBy string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := "inv_" + hex.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.invites[code] = &Invite{
		Code:      code,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}
	return code, nil
}

func (s *MemoryInviteStore) Validate(code string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv, ok := s.invites[code]
	if !ok {
		return fmt.Errorf("invalid invite code")
	}
	if inv.Used {
		return fmt.Errorf("invite already redeemed")
	}
	return nil
}

func (s *MemoryInviteStore) Redeem(code string, orgID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invites[code]
	if !ok {
		return fmt.Errorf("invalid invite code")
	}
	if inv.Used {
		return fmt.Errorf("invite already redeemed")
	}
	inv.Used = true
	inv.RedeemedBy = orgID
	inv.RedeemedAt = time.Now()
	return nil
}

func (s *MemoryInviteStore) List() []*Invite {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Invite, 0, len(s.invites))
	for _, inv := range s.invites {
		out = append(out, inv)
	}
	return out
}
