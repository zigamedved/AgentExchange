// Company C's analyst agent.
// Receives a text report and returns structured metrics: word count,
// key topics, sentiment estimate, and readability score.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/zigamedved/faxp/pkg/platform"
	"github.com/zigamedved/faxp/pkg/protocol"
	"github.com/zigamedved/faxp/pkg/registry"
	faxphttp "github.com/zigamedved/faxp/pkg/transport/http"
)

func main() {
	platformURL := envOr("FAXP_PLATFORM_URL", "http://localhost:8080")
	apiKey := envOr("FAXP_API_KEY", "faxp_companyc_demo")
	agentURL := envOr("FAXP_AGENT_URL", "http://localhost:8083")
	port := envOr("FAXP_AGENT_PORT", "8083")

	agent := &AnalystAgent{
		card: &protocol.AgentCard{
			Name:        "company-c-analyst",
			Description: "Extracts structured metrics and insights from text documents.",
			URL:         agentURL,
			Version:     "1.0.0",
			Skills: []protocol.Skill{
				{
					ID:          "text_analysis",
					Name:        "Text Analysis",
					Description: "Returns word count, key topics, sentiment, and readability from a text document.",
					Tags:        []string{"analysis", "nlp", "metrics", "structured"},
					InputModes:  []string{"text/plain", "text/markdown"},
					OutputModes: []string{"application/json"},
				},
			},
			Capabilities: protocol.AgentCapabilities{
				Streaming: false,
			},
			Authentication: &protocol.AuthSchemes{Schemes: []string{"Bearer"}},
			FaxpPricing: &protocol.Pricing{
				Model:      "per-call",
				PerCallUSD: 0.002,
			},
		},
	}

	// Register with platform
	client := platform.NewPlatformClient(platformURL, apiKey)
	ctx := context.Background()
	agentID, err := client.RegisterAgent(ctx, registry.RegisterRequest{
		Name:        "company-c-analyst",
		EndpointURL: agentURL,
		AgentCard:   *agent.card,
		TTLSeconds:  300,
	})
	if err != nil {
		slog.Warn("could not register with platform", "err", err)
	} else {
		slog.Info("registered with platform", "agent_id", agentID)
		go heartbeat(client, agentID)
	}

	srv := faxphttp.NewServer(agent)
	slog.Info("company-c-analyst agent started", "addr", ":"+port, "endpoint", agentURL)
	if err := http.ListenAndServe(":"+port, srv); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

// AnalystAgent extracts metrics from text.
type AnalystAgent struct {
	card *protocol.AgentCard
}

func (a *AnalystAgent) Card() *protocol.AgentCard { return a.card }

func (a *AnalystAgent) HandleMessage(_ context.Context, params *protocol.SendMessageParams) (*protocol.Task, *protocol.Message, error) {
	text := params.Message.TextContent()
	result := analyzeText(text)

	raw, err := json.Marshal(result)
	if err != nil {
		return nil, nil, err
	}

	trueBool := true
	task := &protocol.Task{
		Status: protocol.TaskStatus{State: protocol.TaskStateCompleted},
		Artifacts: []protocol.Artifact{
			{
				Name:  "analysis.json",
				Parts: []protocol.Part{{Kind: "data", Data: raw}},
				LastChunk: &trueBool,
			},
		},
	}
	return task, nil, nil
}

// AnalysisResult is the structured output of the analyst agent.
type AnalysisResult struct {
	WordCount       int      `json:"word_count"`
	SentenceCount   int      `json:"sentence_count"`
	AvgWordsPerSent float64  `json:"avg_words_per_sentence"`
	KeyTopics       []string `json:"key_topics"`
	Sentiment       string   `json:"sentiment"` // "positive" | "neutral" | "negative"
	SentimentScore  float64  `json:"sentiment_score"` // -1.0 to 1.0
	ReadingTimeMin  float64  `json:"reading_time_min"`
	AnalyzedAt      string   `json:"analyzed_at"`
}

func analyzeText(text string) AnalysisResult {
	words := strings.Fields(text)
	sentences := countSentences(text)
	avgWords := 0.0
	if sentences > 0 {
		avgWords = math.Round(float64(len(words))/float64(sentences)*10) / 10
	}

	sentiment, score := analyzeSentiment(text)
	topics := extractTopics(text)
	readingTime := math.Round(float64(len(words))/200*10) / 10 // 200 wpm average

	return AnalysisResult{
		WordCount:       len(words),
		SentenceCount:   sentences,
		AvgWordsPerSent: avgWords,
		KeyTopics:       topics,
		Sentiment:       sentiment,
		SentimentScore:  score,
		ReadingTimeMin:  readingTime,
		AnalyzedAt:      time.Now().UTC().Format(time.RFC3339),
	}
}

func countSentences(text string) int {
	count := 0
	for _, r := range text {
		if r == '.' || r == '!' || r == '?' {
			count++
		}
	}
	if count == 0 && len(text) > 0 {
		return 1
	}
	return count
}

func analyzeSentiment(text string) (string, float64) {
	lower := strings.ToLower(text)
	positiveWords := []string{"growth", "accelerat", "benefit", "advantage", "success",
		"strong", "efficient", "opportunit", "leading", "improve"}
	negativeWords := []string{"risk", "challenge", "difficult", "pressure", "compliance",
		"concern", "decline", "problem", "barrier", "threat"}

	pos, neg := 0, 0
	for _, w := range positiveWords {
		pos += strings.Count(lower, w)
	}
	for _, w := range negativeWords {
		neg += strings.Count(lower, w)
	}

	total := pos + neg
	if total == 0 {
		return "neutral", 0.0
	}
	score := math.Round(float64(pos-neg)/float64(total)*100) / 100
	switch {
	case score > 0.2:
		return "positive", score
	case score < -0.2:
		return "negative", score
	default:
		return "neutral", score
	}
}

func extractTopics(text string) []string {
	// Extract capitalized multi-word phrases and significant terms
	words := strings.Fields(text)
	freq := make(map[string]int)

	for _, w := range words {
		w = strings.Trim(w, ".,!?:;\"'()[]{}*#")
		if len(w) < 4 {
			continue
		}
		if !unicode.IsLetter(rune(w[0])) {
			continue
		}
		lower := strings.ToLower(w)
		// Skip common stop words
		stop := map[string]bool{
			"this": true, "that": true, "with": true, "from": true,
			"have": true, "will": true, "been": true, "their": true,
			"they": true, "report": true, "which": true, "these": true,
		}
		if stop[lower] {
			continue
		}
		freq[lower]++
	}

	// Return top 5 by frequency
	type kv struct {
		k string
		v int
	}
	var sorted []kv
	for k, v := range freq {
		if v >= 2 {
			sorted = append(sorted, kv{k, v})
		}
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].v > sorted[i].v {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	topics := make([]string, 0, 5)
	for i, kv := range sorted {
		if i >= 5 {
			break
		}
		topics = append(topics, kv.k)
	}
	return topics
}

func heartbeat(client *platform.PlatformClient, agentID string) {
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
