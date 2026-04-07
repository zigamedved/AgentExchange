package registry_test

import (
	"testing"

	"github.com/zigamedved/agent-exchange/pkg/protocol"
	"github.com/zigamedved/agent-exchange/pkg/registry"
)

// runStoreTests runs the same suite against any Store implementation.
// Pass ":memory:" for SQLiteStore, or use NewMemoryStore() directly.
func runStoreTests(t *testing.T, s registry.Store) {
	t.Helper()
	defer s.Close()

	t.Run("RegisterAndGet", func(t *testing.T) {
		id, err := s.Register(registry.RegisterRequest{
			Name:        "agent-a",
			Organization: "org-a",
			EndpointURL:  "http://a",
			Visibility:   registry.AgentPublic,
			AgentCard:    protocol.AgentCard{Name: "Agent A"},
		})
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		entry, ok := s.Get(id)
		if !ok {
			t.Fatal("Get: not found after registration")
		}
		if entry.Name != "agent-a" || entry.Organization != "org-a" {
			t.Errorf("unexpected entry: %+v", entry)
		}
	})

	t.Run("Upsert_ReuseID", func(t *testing.T) {
		r := registry.RegisterRequest{
			Name:        "upsert-agent",
			Organization: "org-u",
			EndpointURL:  "http://v1",
			AgentCard:    protocol.AgentCard{},
		}
		id1, _ := s.Register(r)
		r.EndpointURL = "http://v2"
		id2, _ := s.Register(r)

		if id1 != id2 {
			t.Errorf("re-registration should reuse ID: %s vs %s", id1, id2)
		}
		e, _ := s.Get(id1)
		if e.EndpointURL != "http://v2" {
			t.Errorf("expected updated endpoint, got %s", e.EndpointURL)
		}
	})

	t.Run("Deregister", func(t *testing.T) {
		id, _ := s.Register(registry.RegisterRequest{
			Name: "to-remove", Organization: "org-r",
			EndpointURL: "http://r", AgentCard: protocol.AgentCard{},
		})
		s.Deregister(id)
		if _, ok := s.Get(id); ok {
			t.Error("entry should be gone after Deregister")
		}
	})

	t.Run("Heartbeat", func(t *testing.T) {
		id, _ := s.Register(registry.RegisterRequest{
			Name: "hb", Organization: "org-h",
			EndpointURL: "http://h", AgentCard: protocol.AgentCard{},
		})
		if err := s.Heartbeat(id); err != nil {
			t.Errorf("Heartbeat failed: %v", err)
		}
		if err := s.Heartbeat("nonexistent"); err == nil {
			t.Error("expected error for unknown agent")
		}
	})

	t.Run("Search_BySkill", func(t *testing.T) {
		s.Register(registry.RegisterRequest{
			Name: "skill-agent", Organization: "org-sk",
			EndpointURL: "http://sk",
			AgentCard:   protocol.AgentCard{Skills: []protocol.Skill{{ID: "coding", Tags: []string{"go"}}}},
		})

		results := s.Search(registry.SearchFilter{Skill: "coding"})
		found := false
		for _, r := range results {
			if r.Name == "skill-agent" {
				found = true
			}
		}
		if !found {
			t.Error("expected skill-agent in results")
		}

		// Match by tag
		byTag := s.Search(registry.SearchFilter{Skill: "go"})
		found = false
		for _, r := range byTag {
			if r.Name == "skill-agent" {
				found = true
			}
		}
		if !found {
			t.Error("expected skill-agent when searching by tag")
		}
	})

	t.Run("Search_PrivateVisibility", func(t *testing.T) {
		s.Register(registry.RegisterRequest{
			Name: "private-vis", Organization: "org-vis",
			EndpointURL: "http://vis", Visibility: registry.AgentPrivate,
			AgentCard: protocol.AgentCard{},
		})

		// Other org cannot see it
		others := s.Search(registry.SearchFilter{CallerOrg: "org-other"})
		for _, r := range others {
			if r.Name == "private-vis" {
				t.Error("private agent should not be visible to other org")
			}
		}

		// Owner can see it
		own := s.Search(registry.SearchFilter{CallerOrg: "org-vis"})
		found := false
		for _, r := range own {
			if r.Name == "private-vis" {
				found = true
			}
		}
		if !found {
			t.Error("owner should see their own private agent")
		}
	})
}

func TestMemoryStore_Suite(t *testing.T) {
	runStoreTests(t, registry.NewMemoryStore())
}

func TestSQLiteStore_Suite(t *testing.T) {
	s, err := registry.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	runStoreTests(t, s)
}
