package registry

import (
	"testing"

	"github.com/zigamedved/agent-exchange/pkg/protocol"
)

// helper to build a minimal RegisterRequest
func req(name, org, endpoint string, vis AgentVisibility, skills ...string) RegisterRequest {
	var skillList []protocol.Skill
	for _, s := range skills {
		skillList = append(skillList, protocol.Skill{ID: s, Tags: []string{s + "-tag"}})
	}
	return RegisterRequest{
		Name:        name,
		Organization: org,
		EndpointURL:  endpoint,
		Visibility:   vis,
		AgentCard:    protocol.AgentCard{Skills: skillList},
		TTLSeconds:   300,
	}
}

func TestMemoryStore_Register(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	id, err := s.Register(req("writer", "org-b", "http://localhost:8082", AgentPublic))
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	entry, ok := s.Get(id)
	if !ok {
		t.Fatal("expected entry to exist after registration")
	}
	if entry.Name != "writer" || entry.Organization != "org-b" {
		t.Errorf("unexpected entry: %+v", entry)
	}
}

func TestMemoryStore_Register_MissingFields(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	if _, err := s.Register(RegisterRequest{Organization: "org-a", EndpointURL: "http://x"}); err == nil {
		t.Error("expected error for missing name")
	}
	if _, err := s.Register(RegisterRequest{Name: "foo", Organization: "org-a"}); err == nil {
		t.Error("expected error for missing endpoint")
	}
}

func TestMemoryStore_Upsert_SameID(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	r := req("agent", "org-a", "http://v1", AgentPublic)
	id1, _ := s.Register(r)

	r.EndpointURL = "http://v2"
	id2, _ := s.Register(r)

	if id1 != id2 {
		t.Errorf("re-registration should reuse ID: got %s and %s", id1, id2)
	}

	entry, _ := s.Get(id1)
	if entry.EndpointURL != "http://v2" {
		t.Errorf("expected updated endpoint, got %s", entry.EndpointURL)
	}
}

func TestMemoryStore_Deregister(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	id, _ := s.Register(req("a", "org-a", "http://x", AgentPublic))
	s.Deregister(id)

	if _, ok := s.Get(id); ok {
		t.Error("expected entry to be removed")
	}
}

func TestMemoryStore_Heartbeat(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	id, _ := s.Register(req("a", "org-a", "http://x", AgentPublic))
	if err := s.Heartbeat(id); err != nil {
		t.Errorf("unexpected heartbeat error: %v", err)
	}
	if err := s.Heartbeat("nonexistent"); err == nil {
		t.Error("expected error for unknown agent")
	}
}

func TestMemoryStore_Search_BySkill(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	s.Register(req("writer", "org-b", "http://b", AgentPublic, "writing"))
	s.Register(req("analyst", "org-c", "http://c", AgentPublic, "analysis"))

	results := s.Search(SearchFilter{Skill: "writing"})
	if len(results) != 1 || results[0].Name != "writer" {
		t.Errorf("expected 1 writer, got %v", results)
	}
}

func TestMemoryStore_Search_BySkillTag(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	s.Register(req("writer", "org-b", "http://b", AgentPublic, "writing"))

	// "writing-tag" is the tag set by the helper
	results := s.Search(SearchFilter{Skill: "writing-tag"})
	if len(results) != 1 {
		t.Errorf("expected tag match, got %d results", len(results))
	}
}

func TestMemoryStore_Search_ByName(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	s.Register(req("Super Writer", "org-b", "http://b", AgentPublic))
	s.Register(req("Analyst Pro", "org-c", "http://c", AgentPublic))

	results := s.Search(SearchFilter{Name: "writer"}) // case-insensitive partial
	if len(results) != 1 || results[0].Name != "Super Writer" {
		t.Errorf("expected 1 result for 'writer', got %v", results)
	}
}

func TestMemoryStore_Search_ByOrg(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	s.Register(req("a1", "org-a", "http://a1", AgentPublic))
	s.Register(req("a2", "org-a", "http://a2", AgentPublic))
	s.Register(req("b1", "org-b", "http://b1", AgentPublic))

	results := s.Search(SearchFilter{Organization: "org-a"})
	if len(results) != 2 {
		t.Errorf("expected 2 results for org-a, got %d", len(results))
	}
}

func TestMemoryStore_Search_PrivateVisibility(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	s.Register(req("public-agent", "org-a", "http://a1", AgentPublic))
	s.Register(req("private-agent", "org-a", "http://a2", AgentPrivate))

	// Caller from a different org should not see the private agent
	results := s.Search(SearchFilter{CallerOrg: "org-b"})
	for _, r := range results {
		if r.Name == "private-agent" {
			t.Error("private agent should not be visible to other orgs")
		}
	}

	// Owner org sees both
	results = s.Search(SearchFilter{CallerOrg: "org-a"})
	if len(results) != 2 {
		t.Errorf("owner should see 2 agents, got %d", len(results))
	}
}

func TestMemoryStore_Search_EmptyFilter(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()

	s.Register(req("a", "org-a", "http://a", AgentPublic))
	s.Register(req("b", "org-b", "http://b", AgentPublic))

	results := s.Search(SearchFilter{})
	if len(results) != 2 {
		t.Errorf("expected all 2 agents, got %d", len(results))
	}
}
