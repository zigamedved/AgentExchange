package platform

import (
	"strings"
	"testing"
)

func TestMemoryAuth_SeedAndAuthenticate(t *testing.T) {
	a := NewMemoryAuth(100)
	a.Seed("org-1", "Acme", "key-acme", OrgPublic, 500)

	org := a.Authenticate("key-acme")
	if org == nil {
		t.Fatal("expected org, got nil")
	}
	if org.ID != "org-1" || org.Name != "Acme" {
		t.Errorf("unexpected org: %+v", org)
	}
	if org.Credits != 500 {
		t.Errorf("expected 500 credits, got %v", org.Credits)
	}
}

func TestMemoryAuth_AuthenticateUnknownKey(t *testing.T) {
	a := NewMemoryAuth(100)
	if got := a.Authenticate("does-not-exist"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestMemoryAuth_GetByID(t *testing.T) {
	a := NewMemoryAuth(100)
	a.Seed("org-x", "X Corp", "key-x", OrgPrivate, 0)

	org := a.GetByID("org-x")
	if org == nil {
		t.Fatal("expected org, got nil")
	}
	if org.Name != "X Corp" {
		t.Errorf("unexpected name: %s", org.Name)
	}

	if got := a.GetByID("unknown"); got != nil {
		t.Errorf("expected nil for unknown ID, got %+v", got)
	}
}

func TestMemoryAuth_Register(t *testing.T) {
	a := NewMemoryAuth(50)

	org, err := a.Register("New Co", OrgPrivate)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if org == nil {
		t.Fatal("expected org, got nil")
	}
	if !strings.HasPrefix(org.APIKey, "ax_") {
		t.Errorf("expected key to start with ax_, got %s", org.APIKey)
	}
	if org.Credits != 50 {
		t.Errorf("expected 50 credits, got %v", org.Credits)
	}

	// Must be retrievable
	if got := a.Authenticate(org.APIKey); got == nil {
		t.Error("registered org not found by API key")
	}
	if got := a.GetByID(org.ID); got == nil {
		t.Error("registered org not found by ID")
	}
}

func TestMemoryAuth_Register_EmptyName(t *testing.T) {
	a := NewMemoryAuth(100)
	if _, err := a.Register("", OrgPublic); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestMemoryAuth_DeductCredits(t *testing.T) {
	a := NewMemoryAuth(0)
	a.Seed("org-1", "Foo", "key-1", OrgPublic, 100)

	if err := a.DeductCredits("key-1", 40); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	org := a.Authenticate("key-1")
	if org.Credits != 60 {
		t.Errorf("expected 60 credits, got %v", org.Credits)
	}
}

func TestMemoryAuth_DeductCredits_Insufficient(t *testing.T) {
	a := NewMemoryAuth(0)
	a.Seed("org-1", "Foo", "key-1", OrgPublic, 10)

	if err := a.DeductCredits("key-1", 50); err == nil {
		t.Error("expected insufficient credits error")
	}
}

func TestMemoryAuth_AddCredits(t *testing.T) {
	a := NewMemoryAuth(0)
	a.Seed("org-1", "Foo", "key-1", OrgPublic, 0)

	if err := a.AddCredits("key-1", 25); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if org := a.Authenticate("key-1"); org.Credits != 25 {
		t.Errorf("expected 25 credits, got %v", org.Credits)
	}
}

func TestMemoryAuth_Credits_UnknownKey(t *testing.T) {
	a := NewMemoryAuth(0)

	if err := a.DeductCredits("ghost", 1); err == nil {
		t.Error("expected error for unknown key on DeductCredits")
	}
	if err := a.AddCredits("ghost", 1); err == nil {
		t.Error("expected error for unknown key on AddCredits")
	}
}
