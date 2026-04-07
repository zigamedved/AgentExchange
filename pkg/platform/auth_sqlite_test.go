package platform_test

import (
	"strings"
	"testing"

	"github.com/zigamedved/agent-exchange/pkg/platform"
)

// runAuthTests runs the same Auth test suite against any backend.
func runAuthTests(t *testing.T, a interface {
	platform.Auth
	Seed(id, name, key string, vis platform.OrgVisibility, credits float64)
}) {
	t.Helper()

	t.Run("SeedAndAuthenticate", func(t *testing.T) {
		a.Seed("org-1", "Acme", "key-acme", platform.OrgPublic, 200)
		org := a.Authenticate("key-acme")
		if org == nil {
			t.Fatal("expected org, got nil")
		}
		if org.Name != "Acme" || org.Credits != 200 {
			t.Errorf("unexpected org: %+v", org)
		}
	})

	t.Run("AuthenticateUnknown", func(t *testing.T) {
		if got := a.Authenticate("no-such-key"); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("GetByID", func(t *testing.T) {
		a.Seed("org-x", "X Corp", "key-x", platform.OrgPrivate, 0)
		if org := a.GetByID("org-x"); org == nil || org.Name != "X Corp" {
			t.Errorf("GetByID failed: %+v", org)
		}
		if got := a.GetByID("no-such-id"); got != nil {
			t.Errorf("expected nil for unknown ID, got %+v", got)
		}
	})

	t.Run("Register", func(t *testing.T) {
		org, err := a.Register("New Co", platform.OrgPrivate)
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		if !strings.HasPrefix(org.APIKey, "ax_") {
			t.Errorf("expected ax_ prefix, got %s", org.APIKey)
		}
		if a.Authenticate(org.APIKey) == nil {
			t.Error("registered org not found by API key")
		}
		if a.GetByID(org.ID) == nil {
			t.Error("registered org not found by ID")
		}
	})

	t.Run("DeductAndAddCredits", func(t *testing.T) {
		a.Seed("org-c", "Credits Co", "key-c", platform.OrgPublic, 100)

		if err := a.DeductCredits("key-c", 30); err != nil {
			t.Fatalf("DeductCredits: %v", err)
		}
		if org := a.Authenticate("key-c"); org.Credits != 70 {
			t.Errorf("expected 70, got %v", org.Credits)
		}

		if err := a.AddCredits("key-c", 10); err != nil {
			t.Fatalf("AddCredits: %v", err)
		}
		if org := a.Authenticate("key-c"); org.Credits != 80 {
			t.Errorf("expected 80, got %v", org.Credits)
		}
	})

	t.Run("DeductCredits_Insufficient", func(t *testing.T) {
		a.Seed("org-broke", "Broke Co", "key-broke", platform.OrgPublic, 5)
		if err := a.DeductCredits("key-broke", 100); err == nil {
			t.Error("expected insufficient credits error")
		}
	})
}

func TestMemoryAuth_Suite(t *testing.T) {
	runAuthTests(t, platform.NewMemoryAuth(0))
}

func TestSQLiteAuth_Suite(t *testing.T) {
	a, err := platform.NewSQLiteAuth(":memory:", 0)
	if err != nil {
		t.Fatalf("NewSQLiteAuth: %v", err)
	}
	defer a.Close()
	runAuthTests(t, a)
}

func TestSQLiteInviteStore_SharedDB(t *testing.T) {
	// Verify NewSQLiteInviteStoreFromDB shares the connection correctly.
	auth, err := platform.NewSQLiteAuth(":memory:", 0)
	if err != nil {
		t.Fatalf("NewSQLiteAuth: %v", err)
	}
	defer auth.Close()

	invites, err := platform.NewSQLiteInviteStoreFromDB(auth.DB())
	if err != nil {
		t.Fatalf("NewSQLiteInviteStoreFromDB: %v", err)
	}

	code, err := invites.Create("admin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(code, "inv_") {
		t.Errorf("expected inv_ prefix, got %s", code)
	}
	if err := invites.Validate(code); err != nil {
		t.Errorf("Validate: %v", err)
	}
	if err := invites.Redeem(code, "org-1"); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	// Spent code should fail
	if err := invites.Validate(code); err == nil {
		t.Error("expected error after redemption")
	}
	if err := invites.Redeem(code, "org-2"); err == nil {
		t.Error("expected error on double redeem")
	}

	// Close is a no-op (auth owns the DB)
	if err := invites.Close(); err != nil {
		t.Errorf("Close on shared-DB store: %v", err)
	}
}
