// AgentExchange MCP Server
//
// Exposes the AX platform as MCP tools that Claude Code (or any MCP client) can use.
// On first run, auto-registers a private org and caches credentials to ~/.ax/credentials.
//
// Configure in Claude Code's settings:
//
//	"mcpServers": {
//	  "agent-exchange": {
//	    "command": "go",
//	    "args": ["run", "./cmd/mcp"],
//	    "cwd": "<path-to-agent-exchange>",
//	    "env": {
//	      "AX_PLATFORM_URL": "http://localhost:8080"
//	    }
//	  }
//	}
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zigamedved/agent-exchange/pkg/platform"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
)

func main() {
	platformURL := envOr("AX_PLATFORM_URL", "http://localhost:8080")

	// Try explicit key first, then cached credentials, then auto-register
	apiKey := os.Getenv("AX_API_KEY")
	if apiKey == "" {
		apiKey = loadCachedKey()
	}
	if apiKey == "" {
		var err error
		apiKey, err = autoRegister(platformURL)
		if err != nil {
			// Fall back to demo key if auto-registration fails
			apiKey = "ax_companya_demo"
		}
	}

	s := &mcpServer{
		platformClient: platform.NewPlatformClient(platformURL, apiKey),
		platformURL:    platformURL,
		apiKey:         apiKey,
	}
	s.run()
}

// autoRegister creates a new private org on the platform and caches the API key.
func autoRegister(platformURL string) (string, error) {
	hostname, _ := os.Hostname()
	name := "claude-code-" + hostname

	body, _ := json.Marshal(map[string]string{
		"name":       name,
		"visibility": "private",
	})

	resp, err := http.Post(platformURL+"/platform/v1/orgs", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("registration failed: status %d", resp.StatusCode)
	}

	var result struct {
		APIKey  string  `json:"api_key"`
		Credits float64 `json:"credits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	// Cache the key
	cacheKey(result.APIKey)
	return result.APIKey, nil
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ax", "credentials")
}

func loadCachedKey() string {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func cacheKey(key string) {
	path := credentialsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = os.WriteFile(path, []byte(key+"\n"), 0600)
}

type mcpServer struct {
	platformClient *platform.PlatformClient
	platformURL    string
	apiKey         string
}

// run reads JSON-RPC messages from stdin (LSP wire format) and writes responses to stdout.
func (s *mcpServer) run() {
	reader := bufio.NewReader(os.Stdin)
	for {
		msg, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		var req jsonrpcRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			continue
		}

		var resp any
		switch req.Method {
		case "initialize":
			resp = s.handleInitialize(req.ID)
		case "notifications/initialized":
			continue // notification, no response
		case "tools/list":
			resp = s.handleToolsList(req.ID)
		case "tools/call":
			resp = s.handleToolsCall(req.ID, req.Params)
		case "ping":
			resp = jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{}`)}
		default:
			resp = jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &jsonrpcError{Code: -32601, Message: "method not found: " + req.Method},
			}
		}

		if resp != nil {
			writeMessage(os.Stdout, resp)
		}
	}
}

func (s *mcpServer) handleInitialize(id json.RawMessage) jsonrpcResponse {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "agent-exchange",
			"version": "0.1.0",
		},
	}
	raw, _ := json.Marshal(result)
	return jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: raw}
}

func (s *mcpServer) handleToolsList(id json.RawMessage) jsonrpcResponse {
	tools := []map[string]any{
		{
			"name":        "ax_discover",
			"description": "Discover agents on the AgentExchange platform by skill, name, or organization. Returns a list of agents with their IDs, capabilities, descriptions, and pricing.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill": map[string]any{
						"type":        "string",
						"description": "Filter by skill tag (e.g. 'code-analysis', 'writing', 'research')",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Filter by agent name (partial match)",
					},
					"org": map[string]any{
						"type":        "string",
						"description": "Filter by organization ID",
					},
				},
			},
		},
		{
			"name":        "ax_call",
			"description": "Call an agent on the AgentExchange with a text message. The agent processes the message and returns a response. Use ax_discover first to find the agent ID.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"agent_id": map[string]any{
						"type":        "string",
						"description": "The agent's ID (from ax_discover results)",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "The text message to send to the agent",
					},
				},
				"required": []string{"agent_id", "message"},
			},
		},
		{
			"name":        "ax_list_agents",
			"description": "List all agents currently registered on the AgentExchange platform, with their skills, pricing, and status.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "ax_my_org",
			"description": "Show your organization's info: ID, name, visibility, and remaining credits.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}

	result := map[string]any{"tools": tools}
	raw, _ := json.Marshal(result)
	return jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: raw}
}

func (s *mcpServer) handleToolsCall(id json.RawMessage, params json.RawMessage) jsonrpcResponse {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return errorResponse(id, -32602, "invalid params")
	}

	var text string
	var isError bool

	switch call.Name {
	case "ax_discover":
		text, isError = s.toolDiscover(call.Arguments)
	case "ax_call":
		text, isError = s.toolCall(call.Arguments)
	case "ax_list_agents":
		text, isError = s.toolListAgents()
	case "ax_my_org":
		text, isError = s.toolMyOrg()
	default:
		return errorResponse(id, -32602, "unknown tool: "+call.Name)
	}

	content := []map[string]any{
		{"type": "text", "text": text},
	}
	result := map[string]any{"content": content, "isError": isError}
	raw, _ := json.Marshal(result)
	return jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: raw}
}

func (s *mcpServer) toolDiscover(args json.RawMessage) (string, bool) {
	var params struct {
		Skill string `json:"skill"`
		Name  string `json:"name"`
		Org   string `json:"org"`
	}
	if args != nil {
		_ = json.Unmarshal(args, &params)
	}

	agents, err := s.platformClient.FindAgents(context.Background(), params.Skill)
	if err != nil {
		return fmt.Sprintf("Error discovering agents: %s", err), true
	}

	if len(agents) == 0 {
		return "No agents found matching your criteria.", false
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d agent(s):\n\n", len(agents)))
	for _, a := range agents {
		sb.WriteString(fmt.Sprintf("## %s\n", a.Name))
		sb.WriteString(fmt.Sprintf("- **ID:** `%s`\n", a.ID))
		sb.WriteString(fmt.Sprintf("- **Organization:** %s\n", a.Organization))
		sb.WriteString(fmt.Sprintf("- **Description:** %s\n", a.AgentCard.Description))
		sb.WriteString(fmt.Sprintf("- **Endpoint:** %s\n", a.EndpointURL))
		if a.AgentCard.AXPricing != nil {
			sb.WriteString(fmt.Sprintf("- **Pricing:** %s ($%.4f)\n", a.AgentCard.AXPricing.Model, a.AgentCard.AXPricing.PerCallUSD))
		}
		if len(a.AgentCard.Skills) > 0 {
			sb.WriteString("- **Skills:**\n")
			for _, sk := range a.AgentCard.Skills {
				sb.WriteString(fmt.Sprintf("  - `%s` — %s", sk.ID, sk.Name))
				if len(sk.Tags) > 0 {
					sb.WriteString(fmt.Sprintf(" (tags: %s)", strings.Join(sk.Tags, ", ")))
				}
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String(), false
}

func (s *mcpServer) toolCall(args json.RawMessage) (string, bool) {
	var params struct {
		AgentID string `json:"agent_id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "Invalid arguments: need agent_id and message", true
	}
	if params.AgentID == "" || params.Message == "" {
		return "Both agent_id and message are required", true
	}

	// Build the JSON-RPC request
	sendParams := &protocol.SendMessageParams{
		Message: protocol.NewTextMessage(params.Message),
	}
	rpcReq, err := protocol.NewRequest("1", protocol.MethodSendMessage, sendParams)
	if err != nil {
		return fmt.Sprintf("Error building request: %s", err), true
	}
	body, err := json.Marshal(rpcReq)
	if err != nil {
		return fmt.Sprintf("Error encoding request: %s", err), true
	}

	// Route the call through the platform
	routeURL := s.platformClient.RouteURL(params.AgentID)
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, routeURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Sprintf("Error creating request: %s", err), true
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Sprintf("Error calling agent: %s", err), true
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPaymentRequired {
		return "Insufficient credits. Top up your org to continue calling paid agents.", true
	}

	var rpcResp protocol.Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Sprintf("Error decoding response: %s", err), true
	}
	if rpcResp.Error != nil {
		return fmt.Sprintf("Agent error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message), true
	}

	// Try to parse as Task
	var task protocol.Task
	if err := rpcResp.ParseResult(&task); err == nil && task.Status.State != "" {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Task completed (state: %s)\n\n", task.Status.State))
		for _, artifact := range task.Artifacts {
			if artifact.Name != "" {
				sb.WriteString(fmt.Sprintf("### %s\n", artifact.Name))
			}
			for _, part := range artifact.Parts {
				if part.Kind == "text" {
					sb.WriteString(part.Text)
				}
				if part.Kind == "data" {
					sb.WriteString(string(part.Data))
				}
			}
			sb.WriteString("\n")
		}
		return sb.String(), false
	}

	// Try to parse as Message
	var msg protocol.Message
	if err := rpcResp.ParseResult(&msg); err == nil && msg.Role != "" {
		return msg.TextContent(), false
	}

	return "Agent returned an empty response.", false
}

func (s *mcpServer) toolListAgents() (string, bool) {
	agents, err := s.platformClient.FindAgents(context.Background(), "")
	if err != nil {
		return fmt.Sprintf("Error listing agents: %s", err), true
	}

	if len(agents) == 0 {
		return "No agents currently registered on the platform.", false
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d agent(s) registered:\n\n", len(agents)))
	sb.WriteString(fmt.Sprintf("%-36s  %-24s  %-12s  %s\n", "ID", "Name", "Org", "Price"))
	sb.WriteString(strings.Repeat("-", 90) + "\n")
	for _, a := range agents {
		price := "free"
		if a.AgentCard.AXPricing != nil && a.AgentCard.AXPricing.PerCallUSD > 0 {
			price = fmt.Sprintf("$%.4f/call", a.AgentCard.AXPricing.PerCallUSD)
		}
		sb.WriteString(fmt.Sprintf("%-36s  %-24s  %-12s  %s\n",
			a.ID, a.Name, a.Organization, price))
	}
	return sb.String(), false
}

func (s *mcpServer) toolMyOrg() (string, bool) {
	req, err := http.NewRequest(http.MethodGet, s.platformURL+"/platform/v1/orgs/me", nil)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), true
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Sprintf("Error fetching org info: %s", err), true
	}
	defer resp.Body.Close()

	var org struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		Visibility string  `json:"visibility"`
		Credits    float64 `json:"credits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
		return fmt.Sprintf("Error parsing org info: %s", err), true
	}

	return fmt.Sprintf("**Organization:** %s\n**ID:** %s\n**Visibility:** %s\n**Credits:** $%.4f",
		org.Name, org.ID, org.Visibility, org.Credits), false
}

// ─── JSON-RPC types ─────────────────────────────────────────────────────────

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func errorResponse(id json.RawMessage, code int, msg string) jsonrpcResponse {
	return jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: msg},
	}
}

// ─── LSP wire format (Content-Length framing) ───────────────────────────────

func readMessage(r *bufio.Reader) ([]byte, error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break // end of headers
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, _ = strconv.Atoi(val)
		}
	}
	if contentLength == 0 {
		return nil, fmt.Errorf("no Content-Length header")
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(r, body)
	return body, err
}

func writeMessage(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(data), data)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
