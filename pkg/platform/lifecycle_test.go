package platform_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zigamedved/agent-exchange/pkg/platform"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
	"github.com/zigamedved/agent-exchange/pkg/registry"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newPlatform(t *testing.T) *httptest.Server {
	t.Helper()
	p := platform.New(
		platform.WithRegistration(platform.RegistrationOpen),
		platform.WithDefaultCredits(100),
	)
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, url, apiKey string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func get(t *testing.T, url, apiKey string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func decodeJSON(t *testing.T, r io.Reader, dst any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(dst); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

// createOrg registers a new org and returns the response body.
func createOrg(t *testing.T, platformURL, name, visibility string) map[string]any {
	t.Helper()
	resp := post(t, platformURL+"/platform/v1/orgs", "", map[string]string{
		"name":       name,
		"visibility": visibility,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("createOrg %s: status %d — %s", name, resp.StatusCode, body)
	}
	var out map[string]any
	decodeJSON(t, resp.Body, &out)
	return out
}

// registerAgent registers an agent and returns its assigned ID.
func registerAgent(t *testing.T, platformURL, apiKey, name string, card protocol.AgentCard) string {
	t.Helper()
	resp := post(t, platformURL+"/platform/v1/agents", apiKey, registry.RegisterRequest{
		Name:        name,
		EndpointURL: "http://placeholder",
		AgentCard:   card,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("registerAgent %s: status %d — %s", name, resp.StatusCode, body)
	}
	var out map[string]string
	decodeJSON(t, resp.Body, &out)
	return out["agent_id"]
}

// listAgents returns agents visible to the caller.
func listAgents(t *testing.T, platformURL, apiKey, skill string) []map[string]any {
	t.Helper()
	url := platformURL + "/platform/v1/agents"
	if skill != "" {
		url += "?skill=" + skill
	}
	resp := get(t, url, apiKey)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("listAgents: status %d — %s", resp.StatusCode, body)
	}
	var out struct {
		Agents []map[string]any `json:"agents"`
	}
	decodeJSON(t, resp.Body, &out)
	return out.Agents
}

// ─── tests ───────────────────────────────────────────────────────────────────

func TestLifecycle_OrgRegistration(t *testing.T) {
	srv := newPlatform(t)

	org := createOrg(t, srv.URL, "Acme", "public")

	if org["api_key"] == "" {
		t.Error("expected non-empty api_key")
	}
	if org["id"] == "" {
		t.Error("expected non-empty org id")
	}
	if org["credits"].(float64) != 100 {
		t.Errorf("expected 100 credits, got %v", org["credits"])
	}

	// Registered key should work for authenticated endpoints
	resp := get(t, srv.URL+"/platform/v1/orgs/me", org["api_key"].(string))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /orgs/me, got %d", resp.StatusCode)
	}
}

func TestLifecycle_AuthEnforcement(t *testing.T) {
	srv := newPlatform(t)

	endpoints := []string{
		"/platform/v1/agents",
		"/platform/v1/orgs/me",
	}
	for _, path := range endpoints {
		resp := get(t, srv.URL+path, "") // no key
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s with no key: expected 401, got %d", path, resp.StatusCode)
		}

		resp = get(t, srv.URL+path, "invalid-key")
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s with bad key: expected 401, got %d", path, resp.StatusCode)
		}
	}
}

func TestLifecycle_AgentRegistrationAndDiscovery(t *testing.T) {
	srv := newPlatform(t)

	orgA := createOrg(t, srv.URL, "Org A", "public")
	orgB := createOrg(t, srv.URL, "Org B", "public")
	keyA := orgA["api_key"].(string)
	keyB := orgB["api_key"].(string)

	card := protocol.AgentCard{
		Name: "Writer",
		Skills: []protocol.Skill{
			{ID: "writing", Tags: []string{"content", "copy"}},
		},
	}
	agentID := registerAgent(t, srv.URL, keyA, "Writer", card)
	if agentID == "" {
		t.Fatal("expected agent ID")
	}

	// Org B can see the public agent
	agents := listAgents(t, srv.URL, keyB, "")
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0]["id"] != agentID {
		t.Errorf("unexpected agent id: %v", agents[0]["id"])
	}

	// Filter by skill ID
	bySkill := listAgents(t, srv.URL, keyB, "writing")
	if len(bySkill) != 1 {
		t.Errorf("expected 1 result for skill 'writing', got %d", len(bySkill))
	}

	// Filter by skill tag
	byTag := listAgents(t, srv.URL, keyB, "content")
	if len(byTag) != 1 {
		t.Errorf("expected 1 result for tag 'content', got %d", len(byTag))
	}

	// No match for unknown skill
	byUnknown := listAgents(t, srv.URL, keyB, "unknown-skill")
	if len(byUnknown) != 0 {
		t.Errorf("expected 0 results for unknown skill, got %d", len(byUnknown))
	}
}

func TestLifecycle_PrivateAgentVisibility(t *testing.T) {
	srv := newPlatform(t)

	orgA := createOrg(t, srv.URL, "Org A", "public")
	orgB := createOrg(t, srv.URL, "Org B", "public")
	keyA := orgA["api_key"].(string)
	keyB := orgB["api_key"].(string)

	// Org A registers a private agent
	resp := post(t, srv.URL+"/platform/v1/agents", keyA, registry.RegisterRequest{
		Name:        "Secret Agent",
		EndpointURL: "http://internal",
		Visibility:  registry.AgentPrivate,
		AgentCard:   protocol.AgentCard{Name: "Secret Agent"},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatal("expected 201 for agent registration")
	}

	// Org B cannot see it
	agents := listAgents(t, srv.URL, keyB, "")
	for _, a := range agents {
		if a["name"] == "Secret Agent" {
			t.Error("private agent should not be visible to Org B")
		}
	}

	// Org A can see its own private agent
	ownAgents := listAgents(t, srv.URL, keyA, "")
	found := false
	for _, a := range ownAgents {
		if a["name"] == "Secret Agent" {
			found = true
		}
	}
	if !found {
		t.Error("Org A should see its own private agent")
	}
}

func TestLifecycle_RouteCall(t *testing.T) {
	// Spin up a mock agent that returns a fixed JSON-RPC response
	mockAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":"1","result":{"role":"agent","parts":[{"kind":"text","text":"hello"}]}}`)
	}))
	defer mockAgent.Close()

	srv := newPlatform(t)

	orgA := createOrg(t, srv.URL, "Org A", "public")
	orgB := createOrg(t, srv.URL, "Org B", "public")
	keyA := orgA["api_key"].(string)
	keyB := orgB["api_key"].(string)

	// Org A registers agent pointing at mock
	resp := post(t, srv.URL+"/platform/v1/agents", keyA, registry.RegisterRequest{
		Name:        "Echo",
		EndpointURL: mockAgent.URL,
		AgentCard:   protocol.AgentCard{Name: "Echo"},
	})
	defer resp.Body.Close()
	var reg map[string]string
	decodeJSON(t, resp.Body, &reg)
	agentID := reg["agent_id"]

	// Org B calls the agent through the platform
	rpcBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  "a2a/sendMessage",
		"params":  map[string]any{"message": map[string]any{"role": "user", "parts": []any{map[string]string{"kind": "text", "text": "hi"}}}},
	})
	routeReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/platform/v1/route/"+agentID, bytes.NewReader(rpcBody))
	routeReq.Header.Set("Content-Type", "application/json")
	routeReq.Header.Set("Authorization", "Bearer "+keyB)

	routeResp, err := http.DefaultClient.Do(routeReq)
	if err != nil {
		t.Fatalf("route call failed: %v", err)
	}
	defer routeResp.Body.Close()

	if routeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(routeResp.Body)
		t.Fatalf("expected 200, got %d: %s", routeResp.StatusCode, body)
	}

	// Free agent — Org B credits unchanged
	meResp := get(t, srv.URL+"/platform/v1/orgs/me", keyB)
	defer meResp.Body.Close()
	var me map[string]any
	decodeJSON(t, meResp.Body, &me)
	if me["credits"].(float64) != 100 {
		t.Errorf("expected credits unchanged (free agent), got %v", me["credits"])
	}
}

func TestLifecycle_RouteCall_CreditDeduction(t *testing.T) {
	mockAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":"1","result":{"role":"agent","parts":[{"kind":"text","text":"done"}]}}`)
	}))
	defer mockAgent.Close()

	srv := newPlatform(t)

	orgA := createOrg(t, srv.URL, "Org A", "public")
	orgB := createOrg(t, srv.URL, "Org B", "public")
	keyA := orgA["api_key"].(string)
	keyB := orgB["api_key"].(string)

	// Org A registers a paid agent ($10/call)
	resp := post(t, srv.URL+"/platform/v1/agents", keyA, registry.RegisterRequest{
		Name:        "Paid Agent",
		EndpointURL: mockAgent.URL,
		AgentCard: protocol.AgentCard{
			Name: "Paid Agent",
			AXPricing: &protocol.Pricing{
				Model:      "per-call",
				PerCallUSD: 10,
			},
		},
	})
	defer resp.Body.Close()
	var reg map[string]string
	decodeJSON(t, resp.Body, &reg)
	agentID := reg["agent_id"]

	rpcBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "1",
		"method": "a2a/sendMessage",
		"params": map[string]any{},
	})

	routeReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/platform/v1/route/"+agentID, bytes.NewReader(rpcBody))
	routeReq.Header.Set("Content-Type", "application/json")
	routeReq.Header.Set("Authorization", "Bearer "+keyB)
	routeResp, _ := http.DefaultClient.Do(routeReq)
	routeResp.Body.Close()

	if routeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", routeResp.StatusCode)
	}

	// Org B should now have 90 credits
	meResp := get(t, srv.URL+"/platform/v1/orgs/me", keyB)
	defer meResp.Body.Close()
	var me map[string]any
	decodeJSON(t, meResp.Body, &me)
	if me["credits"].(float64) != 90 {
		t.Errorf("expected 90 credits after $10 call, got %v", me["credits"])
	}
}

func TestLifecycle_RouteCall_InsufficientCredits(t *testing.T) {
	mockAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":"1","result":{}}`)
	}))
	defer mockAgent.Close()

	// Platform with zero default credits
	p := platform.New(
		platform.WithRegistration(platform.RegistrationOpen),
		platform.WithDefaultCredits(0),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	orgA := createOrg(t, srv.URL, "Org A", "public")
	orgB := createOrg(t, srv.URL, "Org B", "public")
	keyA := orgA["api_key"].(string)
	keyB := orgB["api_key"].(string)

	resp := post(t, srv.URL+"/platform/v1/agents", keyA, registry.RegisterRequest{
		Name:        "Expensive",
		EndpointURL: mockAgent.URL,
		AgentCard: protocol.AgentCard{
			AXPricing: &protocol.Pricing{PerCallUSD: 5},
		},
	})
	defer resp.Body.Close()
	var reg map[string]string
	decodeJSON(t, resp.Body, &reg)

	rpcBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "1", "method": "a2a/sendMessage", "params": map[string]any{}})
	routeReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/platform/v1/route/"+reg["agent_id"], bytes.NewReader(rpcBody))
	routeReq.Header.Set("Content-Type", "application/json")
	routeReq.Header.Set("Authorization", "Bearer "+keyB)
	routeResp, _ := http.DefaultClient.Do(routeReq)
	routeResp.Body.Close()

	if routeResp.StatusCode != http.StatusPaymentRequired {
		t.Errorf("expected 402 Payment Required, got %d", routeResp.StatusCode)
	}
}

func TestLifecycle_IntraOrgCall_FreeRegardlessOfPricing(t *testing.T) {
	mockAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"jsonrpc":"2.0","id":"1","result":{}}`)
	}))
	defer mockAgent.Close()

	// Org starts with zero credits
	p := platform.New(
		platform.WithRegistration(platform.RegistrationOpen),
		platform.WithDefaultCredits(0),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	orgA := createOrg(t, srv.URL, "Org A", "public")
	keyA := orgA["api_key"].(string)

	// Register a paid agent under the same org
	resp := post(t, srv.URL+"/platform/v1/agents", keyA, registry.RegisterRequest{
		Name:        "Own Agent",
		EndpointURL: mockAgent.URL,
		AgentCard: protocol.AgentCard{
			AXPricing: &protocol.Pricing{PerCallUSD: 99},
		},
	})
	defer resp.Body.Close()
	var reg map[string]string
	decodeJSON(t, resp.Body, &reg)

	// Calling your own agent should be free — even with $99 pricing and 0 credits
	rpcBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "1", "method": "a2a/sendMessage", "params": map[string]any{}})
	routeReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/platform/v1/route/"+reg["agent_id"], bytes.NewReader(rpcBody))
	routeReq.Header.Set("Content-Type", "application/json")
	routeReq.Header.Set("Authorization", "Bearer "+keyA)
	routeResp, _ := http.DefaultClient.Do(routeReq)
	routeResp.Body.Close()

	if routeResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for intra-org call, got %d", routeResp.StatusCode)
	}
}

func TestLifecycle_ClosedRegistration(t *testing.T) {
	p := platform.New(platform.WithRegistration(platform.RegistrationClosed))
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	resp := post(t, srv.URL+"/platform/v1/orgs", "", map[string]string{"name": "New Org"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for closed registration, got %d", resp.StatusCode)
	}
}

func TestLifecycle_InviteRegistration(t *testing.T) {
	invites := platform.NewMemoryInviteStore()
	p := platform.New(
		platform.WithRegistration(platform.RegistrationInvite),
		platform.WithInviteStore(invites),
	)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	// Without invite → 400
	resp := post(t, srv.URL+"/platform/v1/orgs", "", map[string]string{"name": "Gated Org"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 without invite, got %d", resp.StatusCode)
	}

	// Generate a valid invite
	code, _ := invites.Create("admin")

	// With invite → 201
	resp = post(t, srv.URL+"/platform/v1/orgs", "", map[string]string{
		"name":   "Gated Org",
		"invite": code,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 with valid invite, got %d: %s", resp.StatusCode, body)
	}

	// Reusing the invite → error
	resp2 := post(t, srv.URL+"/platform/v1/orgs", "", map[string]string{
		"name":   "Another Org",
		"invite": code,
	})
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for reused invite, got %d", resp2.StatusCode)
	}
}
