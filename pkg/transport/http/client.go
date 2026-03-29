package axhttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
)

// Client sends A2A-compatible JSON-RPC messages to an agent endpoint.
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// NewClient creates a new agent client. apiKey is optional; provide it when
// calling through the platform proxy (which requires authentication).
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// GetCard fetches the agent's Agent Card from /.well-known/a2a/agent-card.json.
func (c *Client) GetCard(ctx context.Context) (*protocol.AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/.well-known/a2a/agent-card.json", nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get agent card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent card: unexpected status %d", resp.StatusCode)
	}

	var card protocol.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("decode agent card: %w", err)
	}
	return &card, nil
}

// SendMessage calls a2a_sendMessage and returns the Task or Message.
// Returns (task, nil, nil) or (nil, message, nil) depending on the agent's response.
func (c *Client) SendMessage(ctx context.Context, params *protocol.SendMessageParams) (*protocol.Task, *protocol.Message, error) {
	if params.Message.MessageID == "" {
		params.Message.MessageID = uuid.New().String()
	}

	req, err := protocol.NewRequest(uuid.New().String(), protocol.MethodSendMessage, params)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.doRPC(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if resp.Error != nil {
		return nil, nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	// Try to decode as Task first, then Message
	var task protocol.Task
	if err := resp.ParseResult(&task); err == nil && task.ID != "" {
		return &task, nil, nil
	}

	var msg protocol.Message
	if err := resp.ParseResult(&msg); err == nil && msg.Role != "" {
		return nil, &msg, nil
	}

	return nil, nil, fmt.Errorf("unexpected response shape")
}

// StreamMessage calls a2a_sendStreamingMessage and delivers SSE events to the handler.
// The handler is called once per event until the stream ends or ctx is cancelled.
func (c *Client) StreamMessage(ctx context.Context, params *protocol.SendMessageParams, handler func(event map[string]any) error) error {
	if params.Message.MessageID == "" {
		params.Message.MessageID = uuid.New().String()
	}

	req, err := protocol.NewRequest(uuid.New().String(), protocol.MethodSendStreamingMessage, params)
	if err != nil {
		return err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	c.addAuth(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stream: unexpected status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if err := handler(event); err != nil {
			return err
		}

		// Stop on the final event
		if final, ok := event["final"].(bool); ok && final {
			break
		}
	}
	return scanner.Err()
}

// RequestQuote calls ax_quoteRequest to get a price and SLA commitment.
func (c *Client) RequestQuote(ctx context.Context, params *protocol.QuoteRequestParams) (*protocol.QuoteResponse, error) {
	req, err := protocol.NewRequest(uuid.New().String(), protocol.MethodQuoteRequest, params)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var quote protocol.QuoteResponse
	if err := resp.ParseResult(&quote); err != nil {
		return nil, fmt.Errorf("parse quote response: %w", err)
	}
	return &quote, nil
}

func (c *Client) doRPC(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.addAuth(httpReq)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("rpc: %w", err)
	}
	defer httpResp.Body.Close()

	var rpcResp protocol.Response
	if err := json.NewDecoder(httpResp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &rpcResp, nil
}

func (c *Client) addAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
