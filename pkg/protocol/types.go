// Package protocol defines the FAXP message types — a strict superset of
// the A2A Protocol v1.0.0. All A2A-compatible fields are preserved; FAXP
// extensions use the "x-faxp-" prefix and are gracefully ignored by A2A clients.
package protocol

import "encoding/json"

// ─── Agent Card ──────────────────────────────────────────────────────────────

// AgentCard is served at GET /.well-known/agent.json.
// Compatible with the A2A AgentCard schema.
type AgentCard struct {
	Name           string             `json:"name"`
	Description    string             `json:"description,omitempty"`
	URL            string             `json:"url"`
	Version        string             `json:"version"`
	Skills         []Skill            `json:"skills,omitempty"`
	Capabilities   AgentCapabilities  `json:"capabilities"`
	Authentication *AuthSchemes       `json:"authentication,omitempty"`
	// FAXP extensions — ignored by A2A clients
	FaxpPricing *Pricing `json:"x-faxp-pricing,omitempty"`
	FaxpPubKey  string   `json:"x-faxp-pubkey,omitempty"`
}

// Skill describes a specific capability an agent offers.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// AgentCapabilities declares which optional features the agent supports.
type AgentCapabilities struct {
	Streaming         bool `json:"streaming"`
	PushNotifications bool `json:"pushNotifications"`
	// FAXP extension: agent supports quote/request and quote/accept methods
	FaxpQuotes bool `json:"x-faxp-quotes,omitempty"`
}

// AuthSchemes lists the authentication mechanisms the agent accepts.
type AuthSchemes struct {
	Schemes []string `json:"schemes"`
}

// Pricing is the FAXP pricing extension for Agent Cards.
type Pricing struct {
	// Model is one of: "per-call", "per-token", "free"
	Model       string  `json:"model"`
	PerCallUSD  float64 `json:"per_call_usd,omitempty"`
	PerTokenUSD float64 `json:"per_token_usd,omitempty"`
}

// ─── Messages ─────────────────────────────────────────────────────────────────

// Message is an A2A-compatible message with optional FAXP signing metadata.
type Message struct {
	Role      string   `json:"role"` // "user" | "agent"
	Parts     []Part   `json:"parts"`
	MessageID string   `json:"messageId,omitempty"`
	TaskID    string   `json:"taskId,omitempty"`
	ContextID string   `json:"contextId,omitempty"`
	Metadata  Metadata `json:"metadata,omitempty"`
}

// Metadata is an open key-value map. FAXP signing fields live here.
type Metadata map[string]any

// Part is the smallest unit of content in a Message or Artifact.
type Part struct {
	Kind string          `json:"kind"` // "text" | "data" | "file"
	Text string          `json:"text,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
	File *FilePart       `json:"file,omitempty"`
}

// FilePart references or embeds a file.
type FilePart struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Bytes    string `json:"bytes,omitempty"` // base64
}

// NewTextMessage creates a simple user message with a single text part.
func NewTextMessage(text string) Message {
	return Message{
		Role:  "user",
		Parts: []Part{{Kind: "text", Text: text}},
	}
}

// NewDataMessage creates a user message with a structured data part.
func NewDataMessage(data any) (Message, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Role:  "user",
		Parts: []Part{{Kind: "data", Data: raw}},
	}, nil
}

// TextContent extracts concatenated text from all text parts.
func (m *Message) TextContent() string {
	var out string
	for _, p := range m.Parts {
		if p.Kind == "text" {
			out += p.Text
		}
	}
	return out
}

// ─── Tasks ───────────────────────────────────────────────────────────────────

// Task is the A2A unit of work.
type Task struct {
	ID        string     `json:"id"`
	ContextID string     `json:"contextId,omitempty"`
	Status    TaskStatus `json:"status"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Metadata  Metadata   `json:"metadata,omitempty"`
}

// TaskStatus holds the current lifecycle state of a task.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp string    `json:"timestamp,omitempty"`
}

// TaskState represents the A2A task lifecycle.
type TaskState string

const (
	TaskStateSubmitted    TaskState = "submitted"
	TaskStateWorking      TaskState = "working"
	TaskStateCompleted    TaskState = "completed"
	TaskStateFailed       TaskState = "failed"
	TaskStateCanceled     TaskState = "canceled"
	TaskStateRejected     TaskState = "rejected"
	TaskStateAuthRequired TaskState = "auth-required"
	TaskStateInputRequired TaskState = "input-required"
)

// Artifact is an output produced by an agent task.
type Artifact struct {
	ArtifactID  string `json:"artifactId,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parts       []Part `json:"parts"`
	Index       int    `json:"index,omitempty"`
	Append      *bool  `json:"append,omitempty"`
	LastChunk   *bool  `json:"lastChunk,omitempty"`
}

// ─── SSE Streaming Events ────────────────────────────────────────────────────

// TaskStatusUpdateEvent is an SSE event emitted during streaming.
type TaskStatusUpdateEvent struct {
	Kind   string     `json:"kind"` // "status"
	TaskID string     `json:"taskId"`
	Status TaskStatus `json:"status"`
	Final  bool       `json:"final"`
}

// TaskArtifactUpdateEvent carries an artifact chunk during streaming.
type TaskArtifactUpdateEvent struct {
	Kind     string   `json:"kind"` // "artifact"
	ID       string   `json:"id"`
	TaskID   string   `json:"taskId"`
	Artifact Artifact `json:"artifact"`
}

// ─── FAXP Quote Types ────────────────────────────────────────────────────────

// QuoteRequestParams are the params for the quote/request JSON-RPC method.
type QuoteRequestParams struct {
	SkillID         string           `json:"skill_id,omitempty"`
	TaskDescription string           `json:"task_description"`
	InputSample     json.RawMessage  `json:"input_sample,omitempty"`
	Constraints     QuoteConstraints `json:"constraints,omitempty"`
	Nonce           string           `json:"nonce"`
	Timestamp       int64            `json:"timestamp"`
	FromAgent       string           `json:"from_agent"`
}

// QuoteConstraints are the caller's requirements for a quote.
type QuoteConstraints struct {
	MaxPriceUSD float64 `json:"max_price_usd,omitempty"`
	DeadlineMS  int64   `json:"deadline_ms,omitempty"`
}

// QuoteResponse is returned by quote/request.
type QuoteResponse struct {
	QuoteID    string  `json:"quote_id"`
	AgentURI   string  `json:"agent_uri"`
	PriceUSD   float64 `json:"price_usd"`
	SLAMS      int64   `json:"sla_ms"`
	ExpiresAt  int64   `json:"expires_at"`
	Commitment string  `json:"commitment"` // Ed25519 sig (see SPEC §6.1)
}

// QuoteAcceptParams are the params for the quote/accept JSON-RPC method.
type QuoteAcceptParams struct {
	QuoteID string  `json:"quote_id"`
	Message Message `json:"message"`
}

// ─── JSON-RPC helpers ────────────────────────────────────────────────────────

// SendMessageParams are the params for message/send and message/stream.
type SendMessageParams struct {
	Message       Message            `json:"message"`
	Configuration *SendConfiguration `json:"configuration,omitempty"`
	Metadata      Metadata           `json:"metadata,omitempty"`
}

// SendConfiguration provides caller hints for how the response should be shaped.
type SendConfiguration struct {
	AcceptedOutputModes []string `json:"acceptedOutputModes,omitempty"`
	Blocking            *bool    `json:"blocking,omitempty"`
}

// GetTaskParams are the params for tasks/get.
type GetTaskParams struct {
	ID            string `json:"id"`
	HistoryLength *int   `json:"historyLength,omitempty"`
}

// CancelTaskParams are the params for tasks/cancel.
type CancelTaskParams struct {
	ID string `json:"id"`
}
