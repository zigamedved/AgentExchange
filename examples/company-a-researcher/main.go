// Company A's researcher agent.
// This is the demo orchestrator: it discovers agents from the platform registry,
// routes a streaming call to Company B's writer, then routes the output to
// Company C's analyst. All calls go through the platform proxy.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zigamedved/agent-exchange/pkg/platform"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
	"github.com/zigamedved/agent-exchange/pkg/registry"
)

func main() {
	platformURL := envOr("AX_PLATFORM_URL", "http://localhost:8080")
	apiKey := envOr("AX_API_KEY", "ax_companya_demo")
	topic := envOr("AX_TOPIC", "AI Agent Protocols and Distributed Systems in 2026")

	// Wait a moment for agents to finish registering
	time.Sleep(500 * time.Millisecond)

	client := platform.NewPlatformClient(platformURL, apiKey)
	ctx := context.Background()

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("  AgentExchange Demo — Company A Researcher")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("  Topic: %s\n\n", topic)

	// ── Step 1: Discover the writer agent
	fmt.Println("── Step 1: Discovering report_generation agents via platform registry...")
	writers, err := client.FindAgents(ctx, "report_generation")
	must(err, "discover writers")
	if len(writers) == 0 {
		fmt.Println("  x No writer agents found. Is company-b-writer running?")
		os.Exit(1)
	}
	writer := writers[0]
	fmt.Printf("  Found: %s (id: %s)\n\n", writer.Name, writer.ID[:8])

	// ── Step 2: Discover the analyst agent
	fmt.Println("── Step 2: Discovering text_analysis agents via platform registry...")
	analysts, err := client.FindAgents(ctx, "text_analysis")
	must(err, "discover analysts")
	if len(analysts) == 0 {
		fmt.Println("  x No analyst agents found. Is company-c-analyst running?")
		os.Exit(1)
	}
	analyst := analysts[0]
	fmt.Printf("  Found: %s (id: %s)\n\n", analyst.Name, analyst.ID[:8])

	// ── Step 3: Request a streamed report from the writer
	fmt.Println("── Step 3: Routing streaming call > company-b-writer (via platform)...")
	fmt.Println()
	report, err := streamReport(ctx, client, writer, topic, apiKey)
	must(err, "stream report")

	// ── Step 4: Send report to analyst
	fmt.Println()
	fmt.Println("── Step 4: Routing call > company-c-analyst (via platform)...")
	analysis, err := analyzeReport(ctx, client, analyst, report, apiKey)
	must(err, "analyze report")

	// ── Step 5: Print final results
	fmt.Println()
	fmt.Println("── Step 5: Final Analysis Results")
	fmt.Println()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(analysis)

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("  Demo complete. Check the dashboard: %s\n", platformURL)
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println()
}

// streamReport routes a a2a_sendStreamingMessage call through the platform proxy
// and prints the streaming chunks as they arrive.
func streamReport(ctx context.Context, client *platform.PlatformClient, agent *registry.Entry, topic, apiKey string) (string, error) {
	params := protocol.SendMessageParams{
		Message: protocol.Message{
			Role:      "user",
			MessageID: uuid.New().String(),
			Parts:     []protocol.Part{{Kind: "text", Text: "Write a comprehensive research report about: " + topic}},
		},
	}

	rpcReq, err := protocol.NewRequest(uuid.New().String(), protocol.MethodSendStreamingMessage, params)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(rpcReq)

	streamURL := client.RouteStreamURL(agent.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("stream request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("stream response %d: %s", resp.StatusCode, b)
	}

	var report strings.Builder
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

		if event["kind"] == "artifact" {
			if artifact, ok := event["artifact"].(map[string]any); ok {
				if parts, ok := artifact["parts"].([]any); ok {
					for _, p := range parts {
						if part, ok := p.(map[string]any); ok && part["kind"] == "text" {
							chunk := part["text"].(string)
							fmt.Print(chunk)
							report.WriteString(chunk)
						}
					}
				}
			}
		}

		if event["kind"] == "status" && event["final"] == true {
			break
		}
	}
	return report.String(), scanner.Err()
}

// analyzeReport routes a synchronous call to the analyst agent.
func analyzeReport(ctx context.Context, client *platform.PlatformClient, agent *registry.Entry, report, apiKey string) (map[string]any, error) {
	params := protocol.SendMessageParams{
		Message: protocol.Message{
			Role:      "user",
			MessageID: uuid.New().String(),
			Parts:     []protocol.Part{{Kind: "text", Text: report}},
		},
	}

	rpcReq, err := protocol.NewRequest(uuid.New().String(), protocol.MethodSendMessage, params)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(rpcReq)

	routeURL := client.RouteURL(agent.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, routeURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("analyze request: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp protocol.Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode analyze response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// Extract the task result
	var task protocol.Task
	if err := rpcResp.ParseResult(&task); err != nil {
		return nil, err
	}

	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part.Kind == "data" && len(part.Data) > 0 {
				var result map[string]any
				if err := json.Unmarshal(part.Data, &result); err == nil {
					return result, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no data artifact in analyst response")
}

func must(err error, context string) {
	if err != nil {
		slog.Error("fatal error", "context", context, "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
