package platform_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/zigamedved/agent-exchange/pkg/protocol"
	"github.com/zigamedved/agent-exchange/pkg/registry"
)

// ─── GET /platform/v1/agents/{id} ────────────────────────────────────────────

func TestHandleGetAgent_Found(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	key := orgA["api_key"].(string)

	agentID := registerAgent(t, srv.URL, key, "Finder", protocol.AgentCard{Name: "Finder"})

	resp := get(t, srv.URL+"/platform/v1/agents/"+agentID, key)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var entry registry.Entry
	decodeJSON(t, resp.Body, &entry)
	if entry.ID != agentID {
		t.Errorf("expected agent id %s, got %s", agentID, entry.ID)
	}
}

func TestHandleGetAgent_NotFound(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	key := orgA["api_key"].(string)

	resp := get(t, srv.URL+"/platform/v1/agents/does-not-exist", key)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleGetAgent_RequiresAuth(t *testing.T) {
	srv := newPlatform(t)

	resp := get(t, srv.URL+"/platform/v1/agents/any-id", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ─── DELETE /platform/v1/agents/{id} ─────────────────────────────────────────

func TestHandleDeregister_Success(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	key := orgA["api_key"].(string)

	agentID := registerAgent(t, srv.URL, key, "Temporary", protocol.AgentCard{})

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/platform/v1/agents/"+agentID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	// Should no longer be findable
	check := get(t, srv.URL+"/platform/v1/agents/"+agentID, key)
	check.Body.Close()
	if check.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after deregister, got %d", check.StatusCode)
	}
}

func TestHandleDeregister_Forbidden_OtherOrg(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	orgB := createOrg(t, srv.URL, "Org B", "public")
	keyA := orgA["api_key"].(string)
	keyB := orgB["api_key"].(string)

	agentID := registerAgent(t, srv.URL, keyA, "Owned By A", protocol.AgentCard{})

	// Org B tries to delete Org A's agent
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/platform/v1/agents/"+agentID, nil)
	req.Header.Set("Authorization", "Bearer "+keyB)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}

	// Agent should still exist
	check := get(t, srv.URL+"/platform/v1/agents/"+agentID, keyA)
	check.Body.Close()
	if check.StatusCode != http.StatusOK {
		t.Errorf("agent should still exist after blocked delete, got %d", check.StatusCode)
	}
}

func TestHandleDeregister_NotFound(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	key := orgA["api_key"].(string)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/platform/v1/agents/ghost", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// ─── POST /platform/v1/agents/{id}/heartbeat ─────────────────────────────────

func TestHandleHeartbeat_Success(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	key := orgA["api_key"].(string)

	agentID := registerAgent(t, srv.URL, key, "Alive", protocol.AgentCard{})

	resp := post(t, srv.URL+"/platform/v1/agents/"+agentID+"/heartbeat", key, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestHandleHeartbeat_UnknownAgent(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	key := orgA["api_key"].(string)

	resp := post(t, srv.URL+"/platform/v1/agents/ghost/heartbeat", key, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// ─── GET /platform/v1/orgs/me ────────────────────────────────────────────────

func TestHandleGetOrg_ReturnsCorrectOrg(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "My Org", "public")
	key := orgA["api_key"].(string)

	resp := get(t, srv.URL+"/platform/v1/orgs/me", key)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var me map[string]any
	decodeJSON(t, resp.Body, &me)

	if me["name"] != "My Org" {
		t.Errorf("expected 'My Org', got %v", me["name"])
	}
	if me["id"] != orgA["id"] {
		t.Errorf("expected id %v, got %v", orgA["id"], me["id"])
	}
}

// ─── POST /platform/v1/agents (register edge cases) ──────────────────────────

func TestHandleRegister_OrgForcedFromToken(t *testing.T) {
	// Even if a client sends a different organization in the body,
	// the platform must use the org from the API key.
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	orgB := createOrg(t, srv.URL, "Org B", "public")
	keyA := orgA["api_key"].(string)

	// Register with Org A's key but claim to be Org B
	resp := post(t, srv.URL+"/platform/v1/agents", keyA, registry.RegisterRequest{
		Name:         "Spoofed",
		Organization: orgB["id"].(string), // attempt to spoof
		EndpointURL:  "http://fake",
		AgentCard:    protocol.AgentCard{},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatal("expected 201")
	}
	var reg map[string]string
	decodeJSON(t, resp.Body, &reg)

	// The registered agent should belong to Org A, not Org B
	agentResp := get(t, srv.URL+"/platform/v1/agents/"+reg["agent_id"], keyA)
	defer agentResp.Body.Close()
	var entry map[string]any
	decodeJSON(t, agentResp.Body, &entry)

	if entry["organization"] != orgA["id"] {
		t.Errorf("expected org %v, got %v", orgA["id"], entry["organization"])
	}
}

func TestHandleRegister_RequiresAuth(t *testing.T) {
	srv := newPlatform(t)

	resp := post(t, srv.URL+"/platform/v1/agents", "", registry.RegisterRequest{
		Name: "Unauth", EndpointURL: "http://x",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ─── Route edge cases ─────────────────────────────────────────────────────────

func TestHandleRoute_AgentNotFound(t *testing.T) {
	srv := newPlatform(t)
	orgA := createOrg(t, srv.URL, "Org A", "public")
	key := orgA["api_key"].(string)

	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "1", "method": "a2a/sendMessage", "params": map[string]any{}})
	resp := post(t, srv.URL+"/platform/v1/route/nonexistent-agent", key, json.RawMessage(body))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown agent, got %d", resp.StatusCode)
	}
}
