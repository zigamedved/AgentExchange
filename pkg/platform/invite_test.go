package platform

import (
	"strings"
	"testing"
)

func TestMemoryInviteStore_CreateAndValidate(t *testing.T) {
	s := NewMemoryInviteStore()

	code, err := s.Create("admin")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if !strings.HasPrefix(code, "inv_") {
		t.Errorf("expected inv_ prefix, got %s", code)
	}

	if err := s.Validate(code); err != nil {
		t.Errorf("expected valid code, got error: %v", err)
	}
}

func TestMemoryInviteStore_Validate_UnknownCode(t *testing.T) {
	s := NewMemoryInviteStore()
	if err := s.Validate("inv_notreal"); err == nil {
		t.Error("expected error for unknown invite code")
	}
}

func TestMemoryInviteStore_Redeem(t *testing.T) {
	s := NewMemoryInviteStore()

	code, _ := s.Create("admin")

	if err := s.Redeem(code, "org-123"); err != nil {
		t.Fatalf("Redeem failed: %v", err)
	}

	// Code is now spent — Validate should fail
	if err := s.Validate(code); err == nil {
		t.Error("expected error after redemption")
	}

	// Double-redeem should fail
	if err := s.Redeem(code, "org-456"); err == nil {
		t.Error("expected error on double redeem")
	}
}

func TestMemoryInviteStore_Redeem_UnknownCode(t *testing.T) {
	s := NewMemoryInviteStore()
	if err := s.Redeem("inv_fake", "org-1"); err == nil {
		t.Error("expected error for unknown code")
	}
}

func TestMemoryInviteStore_List(t *testing.T) {
	s := NewMemoryInviteStore()

	s.Create("admin")
	s.Create("admin")

	if got := len(s.List()); got != 2 {
		t.Errorf("expected 2 invites, got %d", got)
	}
}
