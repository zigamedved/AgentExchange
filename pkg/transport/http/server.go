// Package faxphttp provides the HTTP transport layer for FAXP agents.
// It implements the A2A-compatible HTTP/JSON-RPC binding and extends it
// with FAXP quote methods and SSE streaming.
package faxphttp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/zigamedved/faxp/pkg/protocol"
)

// Agent is the interface that FAXP agent implementations must satisfy.
type Agent interface {
	// Card returns the agent's capability declaration.
	Card() *protocol.AgentCard

	// HandleMessage processes a synchronous task request.
	// Return either a *Task (async) or a *Message (direct response).
	HandleMessage(ctx context.Context, params *protocol.SendMessageParams) (*protocol.Task, *protocol.Message, error)
}

// StreamingAgent extends Agent with SSE streaming support.
type StreamingAgent interface {
	Agent
	// HandleMessageStream processes a streaming task, writing SSE events to w.
	// The method must call w.WriteStatus(TaskStateCompleted/Failed) to end the stream.
	HandleMessageStream(ctx context.Context, params *protocol.SendMessageParams, w *SSEWriter) error
}

// QuoteAgent extends Agent with FAXP quote negotiation support.
type QuoteAgent interface {
	Agent
	HandleQuoteRequest(ctx context.Context, params *protocol.QuoteRequestParams) (*protocol.QuoteResponse, error)
	HandleQuoteAccept(ctx context.Context, params *protocol.QuoteAcceptParams) (*protocol.Task, *protocol.Message, error)
}

// Server is an HTTP handler that dispatches JSON-RPC requests to an Agent.
// Mount it as the root handler of your agent's HTTP server.
type Server struct {
	agent  Agent
	logger *slog.Logger
}

// NewServer creates a new Server wrapping the given agent.
func NewServer(agent Agent) *Server {
	return &Server{
		agent:  agent,
		logger: slog.Default(),
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/agent.json":
		s.handleAgentCard(w, r)
	case "/health":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	default:
		// Everything else is a JSON-RPC call
		s.handleRPC(w, r)
	}
}

func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	card := s.agent.Card()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(card)
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req protocol.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, nil, protocol.CodeParseError, "invalid JSON", nil)
		return
	}
	if req.JSONRPC != "2.0" {
		s.writeError(w, req.ID, protocol.CodeInvalidRequest, "jsonrpc must be '2.0'", nil)
		return
	}

	s.logger.Info("rpc", "method", req.Method)

	switch req.Method {
	case "message/send":
		s.handleMessageSend(w, r, &req)
	case "message/stream":
		s.handleMessageStream(w, r, &req)
	case "tasks/get":
		s.handleTasksGet(w, r, &req)
	case "tasks/cancel":
		s.writeError(w, req.ID, protocol.CodeUnsupportedOperation, "task cancellation not supported", nil)
	case "quote/request":
		s.handleQuoteRequest(w, r, &req)
	case "quote/accept":
		s.handleQuoteAccept(w, r, &req)
	default:
		s.writeError(w, req.ID, protocol.CodeMethodNotFound, "method not found: "+req.Method, nil)
	}
}

func (s *Server) handleMessageSend(w http.ResponseWriter, _ *http.Request, req *protocol.Request) {
	var params protocol.SendMessageParams
	if err := req.ParseParams(&params); err != nil {
		s.writeError(w, req.ID, protocol.CodeInvalidParams, "invalid params: "+err.Error(), nil)
		return
	}

	task, msg, err := s.agent.HandleMessage(context.Background(), &params)
	if err != nil {
		s.writeError(w, req.ID, protocol.CodeInternalError, err.Error(), nil)
		return
	}

	var result any
	if task != nil {
		result = task
	} else {
		result = msg
	}
	s.writeSuccess(w, req.ID, result)
}

func (s *Server) handleMessageStream(w http.ResponseWriter, r *http.Request, req *protocol.Request) {
	sa, ok := s.agent.(StreamingAgent)
	if !ok {
		s.writeError(w, req.ID, protocol.CodeUnsupportedOperation, "agent does not support streaming", nil)
		return
	}

	var params protocol.SendMessageParams
	if err := req.ParseParams(&params); err != nil {
		s.writeError(w, req.ID, protocol.CodeInvalidParams, "invalid params: "+err.Error(), nil)
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

	writer := &SSEWriter{w: w, flusher: flusher}
	if err := sa.HandleMessageStream(r.Context(), &params, writer); err != nil {
		s.logger.Error("stream error", "err", err)
	}
}

func (s *Server) handleTasksGet(w http.ResponseWriter, _ *http.Request, req *protocol.Request) {
	// Minimal implementation — agents that want full task tracking should override
	s.writeError(w, req.ID, protocol.CodeTaskNotFound, "task not found", nil)
}

func (s *Server) handleQuoteRequest(w http.ResponseWriter, _ *http.Request, req *protocol.Request) {
	qa, ok := s.agent.(QuoteAgent)
	if !ok {
		s.writeError(w, req.ID, protocol.CodeMethodNotFound, "agent does not support quote negotiation", nil)
		return
	}

	var params protocol.QuoteRequestParams
	if err := req.ParseParams(&params); err != nil {
		s.writeError(w, req.ID, protocol.CodeInvalidParams, "invalid params: "+err.Error(), nil)
		return
	}

	resp, err := qa.HandleQuoteRequest(context.Background(), &params)
	if err != nil {
		s.writeError(w, req.ID, protocol.CodeInternalError, err.Error(), nil)
		return
	}
	s.writeSuccess(w, req.ID, resp)
}

func (s *Server) handleQuoteAccept(w http.ResponseWriter, _ *http.Request, req *protocol.Request) {
	qa, ok := s.agent.(QuoteAgent)
	if !ok {
		s.writeError(w, req.ID, protocol.CodeMethodNotFound, "agent does not support quote negotiation", nil)
		return
	}

	var params protocol.QuoteAcceptParams
	if err := req.ParseParams(&params); err != nil {
		s.writeError(w, req.ID, protocol.CodeInvalidParams, "invalid params: "+err.Error(), nil)
		return
	}

	task, msg, err := qa.HandleQuoteAccept(context.Background(), &params)
	if err != nil {
		s.writeError(w, req.ID, protocol.CodeInternalError, err.Error(), nil)
		return
	}

	var result any
	if task != nil {
		result = task
	} else {
		result = msg
	}
	s.writeSuccess(w, req.ID, result)
}

func (s *Server) writeSuccess(w http.ResponseWriter, id json.RawMessage, result any) {
	resp, err := protocol.NewSuccessResponse(id, result)
	if err != nil {
		s.writeError(w, id, protocol.CodeInternalError, "failed to encode response", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string, data any) {
	resp := protocol.NewErrorResponse(id, code, msg, data)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
