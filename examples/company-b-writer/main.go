// Company B's report writer agent.
// Receives structured research data or a topic, returns a formatted markdown report.
// Supports streaming (message/stream) to demonstrate SSE.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zigamedved/faxp/pkg/platform"
	"github.com/zigamedved/faxp/pkg/protocol"
	faxphttp "github.com/zigamedved/faxp/pkg/transport/http"
	"github.com/zigamedved/faxp/pkg/registry"
)

func main() {
	platformURL := envOr("FAXP_PLATFORM_URL", "http://localhost:8080")
	apiKey := envOr("FAXP_API_KEY", "faxp_companyb_demo")
	agentURL := envOr("FAXP_AGENT_URL", "http://localhost:8082")
	port := envOr("FAXP_AGENT_PORT", "8082")

	agent := &WriterAgent{
		card: &protocol.AgentCard{
			Name:        "company-b-writer",
			Description: "Generates structured markdown research reports from topic or data inputs.",
			URL:         agentURL,
			Version:     "1.0.0",
			Skills: []protocol.Skill{
				{
					ID:          "report_generation",
					Name:        "Report Generation",
					Description: "Writes a formatted markdown report from a topic or structured data.",
					Tags:        []string{"writing", "reports", "markdown", "research"},
					InputModes:  []string{"text/plain", "application/json"},
					OutputModes: []string{"text/markdown"},
				},
			},
			Capabilities: protocol.AgentCapabilities{
				Streaming:  true,
				FaxpQuotes: false,
			},
			Authentication: &protocol.AuthSchemes{Schemes: []string{"Bearer"}},
			FaxpPricing: &protocol.Pricing{
				Model:      "per-call",
				PerCallUSD: 0.005,
			},
		},
	}

	// Register with platform
	client := platform.NewPlatformClient(platformURL, apiKey)
	ctx := context.Background()
	agentID, err := client.RegisterAgent(ctx, registry.RegisterRequest{
		Name:        "company-b-writer",
		EndpointURL: agentURL,
		AgentCard:   *agent.card,
		TTLSeconds:  300,
	})
	if err != nil {
		slog.Warn("could not register with platform (is the platform running?)", "err", err)
	} else {
		slog.Info("registered with platform", "agent_id", agentID)
		// Send periodic heartbeats
		go heartbeat(client, agentID)
	}

	srv := faxphttp.NewServer(agent)
	slog.Info("company-b-writer agent started", "addr", ":"+port, "endpoint", agentURL)
	if err := http.ListenAndServe(":"+port, srv); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

// WriterAgent generates markdown reports.
type WriterAgent struct {
	card *protocol.AgentCard
}

func (a *WriterAgent) Card() *protocol.AgentCard { return a.card }

func (a *WriterAgent) HandleMessage(_ context.Context, params *protocol.SendMessageParams) (*protocol.Task, *protocol.Message, error) {
	topic := extractTopic(params.Message)
	report := buildReport(topic)

	trueBool := true
	task := &protocol.Task{
		Status: protocol.TaskStatus{State: protocol.TaskStateCompleted},
		Artifacts: []protocol.Artifact{
			{
				Name:  "report.md",
				Parts: []protocol.Part{{Kind: "text", Text: report}},
				LastChunk: &trueBool,
			},
		},
	}
	return task, nil, nil
}

func (a *WriterAgent) HandleMessageStream(_ context.Context, params *protocol.SendMessageParams, w *faxphttp.SSEWriter) error {
	topic := extractTopic(params.Message)
	chunks := buildReportChunks(topic)

	if err := w.WriteStatus(protocol.TaskStateWorking, false); err != nil {
		return err
	}

	for i, chunk := range chunks {
		time.Sleep(120 * time.Millisecond) // simulate generation latency
		isLast := i == len(chunks)-1
		if err := w.WriteTextChunk(chunk, isLast); err != nil {
			return err
		}
	}

	return w.WriteStatus(protocol.TaskStateCompleted, true)
}

func extractTopic(msg protocol.Message) string {
	text := msg.TextContent()
	if text != "" {
		return text
	}
	return "AI Trends 2026"
}

func buildReport(topic string) string {
	return strings.Join(buildReportChunks(topic), "")
}

func buildReportChunks(topic string) []string {
	title := fmt.Sprintf("# Research Report: %s\n\n", topic)
	exec := fmt.Sprintf(`## Executive Summary

This report provides an analysis of **%s**, covering key trends, market dynamics,
and strategic implications for organizations operating in this space in 2026.

`, topic)
	findings := `## Key Findings

1. **Rapid adoption** — Industry-wide deployment has accelerated significantly, with
   enterprise adoption growing over 3x year-over-year.

2. **Infrastructure maturity** — Foundational tooling and standards have stabilized,
   reducing integration friction for new entrants.

3. **Economic pressure** — Cost-per-capability continues to decline, shifting
   competitive differentiation toward application quality and data advantages.

4. **Regulatory landscape** — Governments in major markets have introduced
   provisional frameworks, creating both compliance requirements and legitimacy.

`
	outlook := `## Strategic Outlook

Organizations that establish standardized integration patterns now will benefit from
compounding efficiency advantages. Key investments should focus on:

- Protocol-layer interoperability
- Observability and cost attribution
- Trust and verification infrastructure

`
	footer := fmt.Sprintf("---\n*Report generated by Company B Writer Agent · %s*\n", time.Now().Format("2006-01-02"))

	return []string{title, exec, findings, outlook, footer}
}

func heartbeat(client *platform.PlatformClient, agentID string) {
	// Refresh every TTL/2 to give a comfortable safety margin before expiry.
	ticker := time.NewTicker(120 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := client.Heartbeat(context.Background(), agentID); err != nil {
			slog.Warn("heartbeat failed", "err", err)
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
