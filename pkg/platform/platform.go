// Package platform implements the AgentExchange platform server — the registry,
// auth, routing proxy, metering, and real-time dashboard backend.
package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
	"github.com/zigamedved/agent-exchange/pkg/registry"
)

// Platform is the central server that orchestrates the AgentExchange agent network.
type Platform struct {
	registry   registry.Store
	auth       *AuthStore
	meter      *Meter
	dashboard  *Dashboard
	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a new Platform with the default in-memory registry store.
func New() *Platform {
	return NewWithStore(registry.NewMemoryStore())
}

// NewWithStore creates a new Platform using the provided registry store.
func NewWithStore(store registry.Store) *Platform {
	p := &Platform{
		registry: store,
		auth:     NewAuthStore(),
		meter:    NewMeter(),
		logger:   slog.Default(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	p.dashboard = &Dashboard{platform: p}
	return p
}

// Handler returns an http.Handler that serves all platform endpoints.
func (p *Platform) Handler() http.Handler {
	mux := http.NewServeMux()

	// Dashboard
	mux.HandleFunc("GET /", p.dashboard.ServeHTTP)
	mux.HandleFunc("GET /platform/v1/events", p.handleEvents)

	// Registry
	mux.HandleFunc("POST /platform/v1/agents", p.handleRegister)
	mux.HandleFunc("GET /platform/v1/agents", p.handleListAgents)
	mux.HandleFunc("GET /platform/v1/agents/{id}", p.handleGetAgent)
	mux.HandleFunc("DELETE /platform/v1/agents/{id}", p.handleDeregister)

	// Routing proxy
	mux.HandleFunc("POST /platform/v1/route/{id}", p.handleRoute)
	mux.HandleFunc("POST /platform/v1/route/{id}/stream", p.handleRouteStream)

	// Heartbeat
	mux.HandleFunc("POST /platform/v1/agents/{id}/heartbeat", p.handleHeartbeat)

	return mux
}

// ─── Registry Endpoints ──────────────────────────────────────────────────────

func (p *Platform) handleRegister(w http.ResponseWriter, r *http.Request) {
	org, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	var req registry.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	req.Organization = org.ID

	id, err := p.registry.Register(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	p.logger.Info("agent registered", "org", org.Name, "name", req.Name, "id", id)
	p.dashboard.Broadcast(Event{
		Kind:      "agent.registered",
		Timestamp: time.Now(),
		Data: map[string]any{
			"agent_id": id,
			"name":     req.Name,
			"org":      org.Name,
			"skills":   req.AgentCard.Skills,
		},
	})

	writeJSON(w, http.StatusCreated, map[string]string{"agent_id": id})
}

func (p *Platform) handleListAgents(w http.ResponseWriter, r *http.Request) {
	_, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	filter := registry.SearchFilter{
		Skill:        r.URL.Query().Get("skill"),
		Organization: r.URL.Query().Get("org"),
		Name:         r.URL.Query().Get("name"),
	}

	var entries []*registry.Entry
	if filter.Skill == "" && filter.Organization == "" && filter.Name == "" {
		entries = p.registry.All()
	} else {
		entries = p.registry.Search(filter)
	}

	writeJSON(w, http.StatusOK, map[string]any{"agents": entries})
}

func (p *Platform) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	_, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	entry, found := p.registry.Get(id)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (p *Platform) handleDeregister(w http.ResponseWriter, r *http.Request) {
	org, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	entry, found := p.registry.Get(id)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}
	if entry.Organization != org.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	p.registry.Deregister(id)
	p.dashboard.Broadcast(Event{
		Kind:      "agent.deregistered",
		Timestamp: time.Now(),
		Data:      map[string]any{"agent_id": id, "name": entry.Name},
	})

	w.WriteHeader(http.StatusNoContent)
}

func (p *Platform) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	_, ok := p.requireAuth(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := p.registry.Heartbeat(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Routing Proxy ───────────────────────────────────────────────────────────

func (p *Platform) handleRoute(w http.ResponseWriter, r *http.Request) {
	org, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	agentID := r.PathValue("id")
	entry, found := p.registry.Get(agentID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	// Extract method for metering
	var rpcReq struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(body, &rpcReq)

	callID := uuid.New().String()
	rec := &CallRecord{
		ID:          callID,
		CallerOrgID: org.ID,
		CallerOrg:   org.Name,
		AgentID:     entry.ID,
		AgentName:   entry.Name,
		AgentOrg:    entry.Organization,
		Method:      rpcReq.Method,
	}
	p.meter.Start(rec)
	p.dashboard.Broadcast(Event{
		Kind:      "call.started",
		Timestamp: time.Now(),
		Data:      callEventData(rec),
	})

	// Proxy to agent
	agentURL := strings.TrimRight(entry.EndpointURL, "/") + "/"
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, agentURL, bytes.NewReader(body))
	if err != nil {
		p.finishCall(callID, false, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "proxy: " + err.Error()})
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("AX-Version", "0.1")
	proxyReq.Header.Set("AX-Caller-Org", org.ID)

	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		p.finishCall(callID, false, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.finishCall(callID, false, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "read response: " + err.Error()})
		return
	}

	priceUSD := callPrice(entry.AgentCard.AXPricing)
	p.finishCall(callID, true, "", priceUSD)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (p *Platform) handleRouteStream(w http.ResponseWriter, r *http.Request) {
	org, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	agentID := r.PathValue("id")
	entry, found := p.registry.Get(agentID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	callID := uuid.New().String()
	rec := &CallRecord{
		ID:          callID,
		CallerOrgID: org.ID,
		CallerOrg:   org.Name,
		AgentID:     entry.ID,
		AgentName:   entry.Name,
		AgentOrg:    entry.Organization,
		Method:      protocol.MethodSendStreamingMessage,
	}
	p.meter.Start(rec)
	p.dashboard.Broadcast(Event{
		Kind:      "call.started",
		Timestamp: time.Now(),
		Data:      callEventData(rec),
	})

	// Rewrite method to a2a_sendStreamingMessage
	var rpcMsg map[string]any
	if err := json.Unmarshal(body, &rpcMsg); err == nil {
		rpcMsg["method"] = protocol.MethodSendStreamingMessage
		body, _ = json.Marshal(rpcMsg)
	}

	agentURL := strings.TrimRight(entry.EndpointURL, "/") + "/"
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, agentURL, bytes.NewReader(body))
	if err != nil {
		p.finishCall(callID, false, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "proxy: " + err.Error()})
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("Accept", "text/event-stream")
	proxyReq.Header.Set("AX-Version", "0.1")
	proxyReq.Header.Set("AX-Caller-Org", org.ID)

	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		p.finishCall(callID, false, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, _ := w.(http.Flusher)

	_, _ = io.Copy(&flushWriter{w: w, flusher: flusher}, resp.Body)

	priceUSD := callPrice(entry.AgentCard.AXPricing)
	p.finishCall(callID, true, "", priceUSD)
}

func (p *Platform) finishCall(callID string, success bool, errMsg string, price float64) {
	rec := p.meter.Complete(callID, success, errMsg, price)
	if rec == nil {
		return
	}
	kind := "call.completed"
	if !success {
		kind = "call.failed"
	}
	p.dashboard.Broadcast(Event{
		Kind:      kind,
		Timestamp: time.Now(),
		Data:      callEventData(rec),
	})
}

// ─── SSE Event Stream ────────────────────────────────────────────────────────

func (p *Platform) handleEvents(w http.ResponseWriter, r *http.Request) {
	_, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sub := p.dashboard.Subscribe()
	defer p.dashboard.Unsubscribe(sub)

	// Send current state as initial payload
	initial := map[string]any{
		"kind":   "init",
		"agents": p.registry.All(),
		"calls":  p.meter.Recent(20),
		"spend":  p.meter.SpendByOrg(),
	}
	if data, err := json.Marshal(initial); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-sub:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ─── Auth Helper ─────────────────────────────────────────────────────────────

func (p *Platform) requireAuth(w http.ResponseWriter, r *http.Request) (*Org, bool) {
	key := extractBearerToken(r)
	if key == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing Authorization header"})
		return nil, false
	}
	org := p.auth.Authenticate(key)
	if org == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
		return nil, false
	}
	return org, true
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return r.URL.Query().Get("api_key")
}

// ─── Dashboard ───────────────────────────────────────────────────────────────

// Event is a platform event broadcast to dashboard subscribers.
type Event struct {
	Kind      string         `json:"kind"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// Dashboard manages real-time SSE subscriptions for the web dashboard.
type Dashboard struct {
	platform *Platform
	mu       sync.RWMutex
	subs     map[chan Event]struct{}
}

func (d *Dashboard) Subscribe() chan Event {
	ch := make(chan Event, 32)
	d.mu.Lock()
	if d.subs == nil {
		d.subs = make(map[chan Event]struct{})
	}
	d.subs[ch] = struct{}{}
	d.mu.Unlock()
	return ch
}

func (d *Dashboard) Unsubscribe(ch chan Event) {
	d.mu.Lock()
	delete(d.subs, ch)
	d.mu.Unlock()
	close(ch)
}

func (d *Dashboard) Broadcast(e Event) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for ch := range d.subs {
		select {
		case ch <- e:
		default:
			// drop if subscriber is slow
		}
	}
}

func (d *Dashboard) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func callEventData(r *CallRecord) map[string]any {
	return map[string]any{
		"id":           r.ID,
		"caller_org":   r.CallerOrg,
		"agent_name":   r.AgentName,
		"agent_org":    r.AgentOrg,
		"method":       r.Method,
		"status":       r.Status,
		"latency_ms":   r.LatencyMS,
		"price_usd":    r.PriceUSD,
		"started_at":   r.StartedAt,
		"completed_at": r.CompletedAt,
	}
}

type flushWriter struct {
	w       io.Writer
	flusher http.Flusher
}

func (fw *flushWriter) Write(p []byte) (n int, err error) {
	n, err = fw.w.Write(p)
	if fw.flusher != nil {
		fw.flusher.Flush()
	}
	return
}

// callPrice returns the per-call price from an agent card, or 0 if not set.
func callPrice(pricing *protocol.Pricing) float64 {
	if pricing == nil {
		return 0
	}
	return pricing.PerCallUSD
}

// PlatformClient lets agents interact with the platform (register, discover).
type PlatformClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewPlatformClient creates a client for the platform API.
func NewPlatformClient(baseURL, apiKey string) *PlatformClient {
	return &PlatformClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// RegisterAgent registers this agent with the platform and returns the agent ID.
func (c *PlatformClient) RegisterAgent(ctx context.Context, req registry.RegisterRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/platform/v1/agents", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("register failed (status %d): %s", resp.StatusCode, body)
	}

	var result struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode register response: %w", err)
	}
	return result.AgentID, nil
}

// FindAgents discovers agents matching the given skill tag.
func (c *PlatformClient) FindAgents(ctx context.Context, skill string) ([]*registry.Entry, error) {
	url := c.baseURL + "/platform/v1/agents?skill=" + skill
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("find agents: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Agents []*registry.Entry `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode agents: %w", err)
	}
	return result.Agents, nil
}

// Heartbeat refreshes an agent's TTL so it is not reaped by the registry.
// Call this periodically (e.g. every TTL/2 seconds) from a background goroutine.
func (c *PlatformClient) Heartbeat(ctx context.Context, agentID string) error {
	url := fmt.Sprintf("%s/platform/v1/agents/%s/heartbeat", c.baseURL, agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heartbeat failed (status %d): %s", resp.StatusCode, body)
	}
	return nil
}

// RouteURL returns the platform routing URL for a given agent ID.
func (c *PlatformClient) RouteURL(agentID string) string {
	return fmt.Sprintf("%s/platform/v1/route/%s", c.baseURL, agentID)
}

// RouteStreamURL returns the streaming routing URL for a given agent ID.
func (c *PlatformClient) RouteStreamURL(agentID string) string {
	return fmt.Sprintf("%s/platform/v1/route/%s/stream", c.baseURL, agentID)
}
