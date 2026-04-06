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
	"github.com/zigamedved/agent-exchange/pkg/identity"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
	"github.com/zigamedved/agent-exchange/pkg/registry"
)

// CallHook is invoked after each routed call completes.
// Use it to implement custom billing, logging, or metrics.
type CallHook func(rec *CallRecord)

// Config holds platform configuration. Use Option functions to set fields.
type Config struct {
	Store            registry.Store
	Auth             Auth
	Invites          InviteStore
	Registration     RegistrationMode
	DefaultCredits   float64
	OnCall           CallHook
	Logger           *slog.Logger
	VerifySignatures bool // if true, reject messages with invalid signatures
}

// Option configures a Platform.
type Option func(*Config)

// WithStore sets the registry store backend.
func WithStore(s registry.Store) Option {
	return func(c *Config) { c.Store = s }
}

// WithAuth sets the authentication backend.
func WithAuth(a Auth) Option {
	return func(c *Config) { c.Auth = a }
}

// WithRegistration sets how new orgs are created.
func WithRegistration(mode RegistrationMode) Option {
	return func(c *Config) { c.Registration = mode }
}

// WithDefaultCredits sets the credits given to new orgs.
func WithDefaultCredits(credits float64) Option {
	return func(c *Config) { c.DefaultCredits = credits }
}

// WithOnCall sets a hook that fires after each routed call.
func WithOnCall(hook CallHook) Option {
	return func(c *Config) { c.OnCall = hook }
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option {
	return func(c *Config) { c.Logger = l }
}

// WithSignatureVerification enables rejecting messages with invalid signatures.
func WithSignatureVerification(enabled bool) Option {
	return func(c *Config) { c.VerifySignatures = enabled }
}

// WithInviteStore sets the invite store for invite-gated registration.
func WithInviteStore(s InviteStore) Option {
	return func(c *Config) { c.Invites = s }
}

// Platform is the central server that orchestrates the AgentExchange agent network.
type Platform struct {
	config     Config
	registry   registry.Store
	auth       Auth
	meter      *Meter
	dashboard  *Dashboard
	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a new Platform with the given options.
// With no options, it uses in-memory storage, open registration, and 100 free credits.
func New(opts ...Option) *Platform {
	cfg := Config{
		Store:          registry.NewMemoryStore(),
		Registration:   RegistrationOpen,
		DefaultCredits: 100,
		Logger:         slog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Default auth: in-memory with configured default credits
	if cfg.Auth == nil {
		cfg.Auth = NewMemoryAuth(cfg.DefaultCredits)
	}

	p := &Platform{
		config:   cfg,
		registry: cfg.Store,
		auth:     cfg.Auth,
		meter:    NewMeter(),
		logger:   cfg.Logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	p.dashboard = &Dashboard{platform: p}
	return p
}

// Auth returns the platform's auth backend (useful for seeding orgs from cmd/).
func (p *Platform) Auth() Auth {
	return p.auth
}

// Handler returns an http.Handler that serves all platform endpoints.
func (p *Platform) Handler() http.Handler {
	mux := http.NewServeMux()

	// Dashboard
	mux.HandleFunc("GET /", p.dashboard.ServeHTTP)
	mux.HandleFunc("GET /platform/v1/events", p.handleEvents)

	// Org management (self-service)
	mux.HandleFunc("POST /platform/v1/orgs", p.handleCreateOrg)
	mux.HandleFunc("GET /platform/v1/orgs/me", p.handleGetOrg)

	// Registry
	mux.HandleFunc("POST /platform/v1/agents", p.handleRegister)
	mux.HandleFunc("GET /platform/v1/agents", p.handleListAgents)
	mux.HandleFunc("GET /platform/v1/agents/{id}", p.handleGetAgent)
	mux.HandleFunc("DELETE /platform/v1/agents/{id}", p.handleDeregister)

	// Routing proxy
	mux.HandleFunc("POST /platform/v1/route/{id}", p.handleRoute)
	mux.HandleFunc("POST /platform/v1/route/{id}/stream", p.handleRouteStream)

	// Task lifecycle (proxied to agents)
	mux.HandleFunc("POST /platform/v1/tasks/{taskId}", p.handleGetTask)
	mux.HandleFunc("POST /platform/v1/tasks/{taskId}/cancel", p.handleCancelTask)

	// Heartbeat
	mux.HandleFunc("POST /platform/v1/agents/{id}/heartbeat", p.handleHeartbeat)

	return mux
}

// ─── Org Endpoints ──────────────────────────────────────────────────────────

func (p *Platform) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		Visibility string `json:"visibility"` // "public" or "private", default "private"
		Invite     string `json:"invite"`     // required when registration is "invite"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	// Check registration mode
	switch p.config.Registration {
	case RegistrationClosed:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "registration is closed"})
		return
	case RegistrationInvite:
		if p.config.Invites == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invite store not configured"})
			return
		}
		if req.Invite == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invite code required"})
			return
		}
		if err := p.config.Invites.Validate(req.Invite); err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
	}

	vis := OrgPrivate
	if req.Visibility == "public" {
		vis = OrgPublic
	}

	org, err := p.auth.Register(req.Name, vis)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Redeem the invite after successful org creation
	if p.config.Registration == RegistrationInvite && p.config.Invites != nil {
		_ = p.config.Invites.Redeem(req.Invite, org.ID)
	}

	p.logger.Info("org created", "id", org.ID, "name", org.Name, "visibility", org.Visibility)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         org.ID,
		"name":       org.Name,
		"api_key":    org.APIKey,
		"visibility": org.Visibility,
		"credits":    org.Credits,
	})
}

func (p *Platform) handleGetOrg(w http.ResponseWriter, r *http.Request) {
	org, ok := p.requireAuth(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         org.ID,
		"name":       org.Name,
		"visibility": org.Visibility,
		"credits":    org.Credits,
		"created_at": org.CreatedAt,
	})
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
			"agent_id":    id,
			"name":        req.Name,
			"org":         org.Name,
			"visibility":  string(req.Visibility),
			"description": req.AgentCard.Description,
			"skills":      req.AgentCard.Skills,
			"pricing":     req.AgentCard.AXPricing,
		},
	})

	writeJSON(w, http.StatusCreated, map[string]string{"agent_id": id})
}

func (p *Platform) handleListAgents(w http.ResponseWriter, r *http.Request) {
	org, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	filter := registry.SearchFilter{
		Skill:        r.URL.Query().Get("skill"),
		Organization: r.URL.Query().Get("org"),
		Name:         r.URL.Query().Get("name"),
		CallerOrg:    org.ID,
	}

	entries := p.registry.Search(filter)
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

	// Verify message signature if present
	if err := p.verifyMessageSignature(body); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	// Extract method for metering
	var rpcReq struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	_ = json.Unmarshal(body, &rpcReq)

	// Determine pricing — intra-org calls are free
	intraOrg := strings.EqualFold(org.ID, entry.Organization)
	priceUSD := 0.0
	if !intraOrg {
		// If this is a quote-accepted call, use the quoted price
		if rpcReq.Method == protocol.MethodQuoteAccept {
			var qp struct {
				QuoteID string `json:"quote_id"`
			}
			_ = json.Unmarshal(rpcReq.Params, &qp)
			if qr := p.meter.GetQuote(qp.QuoteID); qr != nil {
				priceUSD = qr.PriceUSD
			}
		} else if rpcReq.Method == protocol.MethodQuoteRequest {
			// Quote requests are free — you're just asking for a price
			priceUSD = 0
		} else {
			priceUSD = callPrice(entry.AgentCard.AXPricing)
		}
	}

	// Check credits before routing (skip for intra-org)
	if !intraOrg && priceUSD > 0 {
		if err := p.auth.DeductCredits(extractBearerToken(r), priceUSD); err != nil {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"error": err.Error()})
			return
		}
	}

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
		// Refund credits on proxy failure
		if !intraOrg && priceUSD > 0 {
			_ = p.auth.AddCredits(extractBearerToken(r), priceUSD)
		}
		p.finishCall(callID, false, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "proxy: " + err.Error()})
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("AX-Version", "0.1")
	proxyReq.Header.Set("AX-Caller-Org", org.ID)

	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		// Refund credits on upstream failure
		if !intraOrg && priceUSD > 0 {
			_ = p.auth.AddCredits(extractBearerToken(r), priceUSD)
		}
		p.finishCall(callID, false, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		if !intraOrg && priceUSD > 0 {
			_ = p.auth.AddCredits(extractBearerToken(r), priceUSD)
		}
		p.finishCall(callID, false, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "read response: " + err.Error()})
		return
	}

	p.finishCall(callID, true, "", priceUSD)

	// Capture quote responses for price binding
	if rpcReq.Method == protocol.MethodQuoteRequest {
		var rpcResp struct {
			Result protocol.QuoteResponse `json:"result"`
		}
		if err := json.Unmarshal(respBody, &rpcResp); err == nil && rpcResp.Result.QuoteID != "" {
			p.meter.StoreQuote(&QuoteRecord{
				QuoteID:   rpcResp.Result.QuoteID,
				AgentID:   entry.ID,
				CallerOrg: org.ID,
				PriceUSD:  rpcResp.Result.PriceUSD,
				SLAMS:     rpcResp.Result.SLAMS,
				ExpiresAt: rpcResp.Result.ExpiresAt,
			})
			p.logger.Info("quote captured", "quote_id", rpcResp.Result.QuoteID, "price", rpcResp.Result.PriceUSD)
		}
	}

	// Track tasks from sendMessage responses
	if rpcReq.Method == protocol.MethodSendMessage || rpcReq.Method == protocol.MethodQuoteAccept {
		var rpcResp struct {
			Result protocol.Task `json:"result"`
		}
		if err := json.Unmarshal(respBody, &rpcResp); err == nil && rpcResp.Result.ID != "" {
			p.meter.StoreTask(&TaskRecord{
				TaskID:    rpcResp.Result.ID,
				AgentID:   entry.ID,
				CallerOrg: org.ID,
				State:     string(rpcResp.Result.Status.State),
			})
		}
	}

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

	// Verify message signature if present
	if err := p.verifyMessageSignature(body); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	// Intra-org calls are free
	intraOrg := strings.EqualFold(org.ID, entry.Organization)
	priceUSD := 0.0
	if !intraOrg {
		priceUSD = callPrice(entry.AgentCard.AXPricing)
	}

	if !intraOrg && priceUSD > 0 {
		if err := p.auth.DeductCredits(extractBearerToken(r), priceUSD); err != nil {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"error": err.Error()})
			return
		}
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
		if !intraOrg && priceUSD > 0 {
			_ = p.auth.AddCredits(extractBearerToken(r), priceUSD)
		}
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
		if !intraOrg && priceUSD > 0 {
			_ = p.auth.AddCredits(extractBearerToken(r), priceUSD)
		}
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

	p.finishCall(callID, true, "", priceUSD)
}

// ─── Task Lifecycle ─────────────────────────────────────────────────────────

func (p *Platform) handleGetTask(w http.ResponseWriter, r *http.Request) {
	org, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	taskID := r.PathValue("taskId")
	tr := p.meter.GetTask(taskID)
	if tr == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	// Only the caller who created the task can check it
	if tr.CallerOrg != org.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	// Proxy a2a_getTask to the agent that owns it
	entry, found := p.registry.Get(tr.AgentID)
	if !found {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "agent no longer registered"})
		return
	}

	rpcReq, _ := protocol.NewRequest("1", protocol.MethodGetTask, &protocol.GetTaskParams{ID: taskID})
	body, _ := json.Marshal(rpcReq)

	agentURL := strings.TrimRight(entry.EndpointURL, "/") + "/"
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, agentURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "proxy: " + err.Error()})
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("AX-Version", "0.1")

	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "read response: " + err.Error()})
		return
	}

	// Update cached task state from the agent's response
	var rpcResp struct {
		Result protocol.Task `json:"result"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err == nil && rpcResp.Result.ID != "" {
		p.meter.UpdateTaskState(taskID, string(rpcResp.Result.Status.State))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (p *Platform) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	org, ok := p.requireAuth(w, r)
	if !ok {
		return
	}

	taskID := r.PathValue("taskId")
	tr := p.meter.GetTask(taskID)
	if tr == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	if tr.CallerOrg != org.ID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	entry, found := p.registry.Get(tr.AgentID)
	if !found {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "agent no longer registered"})
		return
	}

	rpcReq, _ := protocol.NewRequest("1", protocol.MethodCancelTask, &protocol.CancelTaskParams{ID: taskID})
	body, _ := json.Marshal(rpcReq)

	agentURL := strings.TrimRight(entry.EndpointURL, "/") + "/"
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, agentURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "proxy: " + err.Error()})
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("AX-Version", "0.1")

	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "read response: " + err.Error()})
		return
	}

	// Update cached state
	var rpcResp struct {
		Result protocol.Task `json:"result"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err == nil && rpcResp.Result.ID != "" {
		p.meter.UpdateTaskState(taskID, string(rpcResp.Result.Status.State))
	} else {
		// If agent confirmed cancellation without returning a task
		p.meter.UpdateTaskState(taskID, string(protocol.TaskStateCanceled))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (p *Platform) finishCall(callID string, success bool, errMsg string, price float64) {
	rec := p.meter.Complete(callID, success, errMsg, price)
	if rec == nil {
		return
	}

	// Invoke billing hook
	if p.config.OnCall != nil {
		p.config.OnCall(rec)
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

// verifyMessageSignature checks the x-ax-sig metadata on a routed message body.
// If the message has no signature metadata, verification is skipped (signatures are optional).
// If a signature is present but invalid, an error is returned.
func (p *Platform) verifyMessageSignature(body []byte) error {
	// Parse the JSON-RPC request to extract params.message.metadata
	var rpc struct {
		Method string `json:"method"`
		Params struct {
			Message struct {
				Metadata map[string]any `json:"metadata"`
			} `json:"message"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &rpc); err != nil {
		return nil // not a valid RPC, let the downstream handle it
	}

	meta := rpc.Params.Message.Metadata
	if meta == nil {
		return nil // no metadata, signing is optional
	}

	sigB64, _ := meta["x-ax-sig"].(string)
	if sigB64 == "" {
		return nil // no signature present, that's fine
	}

	// Extract signing fields from metadata
	fromURI, _ := meta["x-ax-from"].(string)
	nonce, _ := meta["x-ax-nonce"].(string)
	tsFloat, _ := meta["x-ax-ts"].(float64)
	ts := int64(tsFloat)

	if fromURI == "" || nonce == "" || ts == 0 {
		return fmt.Errorf("incomplete signing metadata: need x-ax-from, x-ax-nonce, x-ax-ts")
	}

	// Parse the agent URI to look up the sender's public key
	// Format: agent://org/name
	parts := strings.SplitN(strings.TrimPrefix(fromURI, "agent://"), "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid agent URI: %s", fromURI)
	}
	senderOrg, senderName := parts[0], parts[1]

	entry, found := p.registry.GetByName(senderOrg, senderName)
	if !found {
		// Sender not in registry — can't verify, but don't block
		p.logger.Warn("signature present but sender not in registry", "from", fromURI)
		return nil
	}

	pubKey := entry.AgentCard.AXPubKey
	if pubKey == "" {
		// Agent has no public key — can't verify
		p.logger.Warn("signature present but agent has no public key", "from", fromURI)
		return nil
	}

	// Re-serialize params for payload hash
	var fullRPC struct {
		Params json.RawMessage `json:"params"`
	}
	_ = json.Unmarshal(body, &fullRPC)

	// Determine the "to" from the method context (not available here, use empty)
	// The spec says "to" is the target agent URI — we use "" for platform-routed calls
	err := identity.VerifyMessage(pubKey, fromURI, "", rpc.Method, nonce, ts, fullRPC.Params, sigB64)
	if err != nil {
		return fmt.Errorf("signature verification failed for %s: %w", fromURI, err)
	}

	p.logger.Info("signature verified", "from", fromURI)
	return nil
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
