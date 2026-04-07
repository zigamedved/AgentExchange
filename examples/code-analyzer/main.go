// Code Analyzer agent for AgentExchange.
//
// Accepts source code as text, returns analysis including:
// - Language detection
// - Line counts (total, code, blank, comment)
// - Function/method detection
// - Import/dependency listing
// - TODO/FIXME/HACK detection
// - Basic complexity assessment
//
// No LLM required — pure static analysis.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/zigamedved/agent-exchange/pkg/platform"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
	"github.com/zigamedved/agent-exchange/pkg/registry"
	axhttp "github.com/zigamedved/agent-exchange/pkg/transport/http"
)

func main() {
	platformURL := envOr("AX_PLATFORM_URL", "http://localhost:8080")
	apiKey := envOr("AX_API_KEY", "ax_companyb_demo")
	agentURL := envOr("AX_AGENT_URL", "http://localhost:8084")
	port := envOr("AX_AGENT_PORT", "8084")

	agent := &CodeAnalyzer{
		card: &protocol.AgentCard{
			Name:        "code-analyzer",
			Description: "Analyzes source code and returns metrics: line counts, function detection, dependency listing, TODO scanning, and complexity assessment. Supports Go, Python, JavaScript, TypeScript, Rust, and Java.",
			URL:         agentURL,
			Version:     "1.0.0",
			Skills: []protocol.Skill{
				{
					ID:          "code_analysis",
					Name:        "Code Analysis",
					Description: "Static analysis of source code — line counts, function detection, imports, TODOs, complexity.",
					Tags:        []string{"code-analysis", "code", "analysis", "static-analysis", "metrics", "code-review"},
					InputModes:  []string{"text/plain"},
					OutputModes: []string{"text/markdown"},
				},
			},
			Capabilities: protocol.AgentCapabilities{
				Streaming: false,
				AXQuotes:  false,
			},
			AXPricing: &protocol.Pricing{
				Model:      "per-call",
				PerCallUSD: 0.001,
			},
		},
	}

	client := platform.NewPlatformClient(platformURL, apiKey)
	ctx := context.Background()
	agentID, err := client.RegisterAgent(ctx, registry.RegisterRequest{
		Name:        "code-analyzer",
		EndpointURL: agentURL,
		AgentCard:   *agent.card,
		TTLSeconds:  300,
	})
	if err != nil {
		slog.Warn("could not register with platform (is it running?)", "err", err)
	} else {
		slog.Info("registered with platform", "agent_id", agentID)
		go heartbeat(client, agentID)
	}

	srv := axhttp.NewServer(agent)
	slog.Info("code-analyzer agent started", "addr", ":"+port, "endpoint", agentURL)
	if err := http.ListenAndServe(":"+port, srv); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

// CodeAnalyzer implements the Agent interface.
type CodeAnalyzer struct {
	card *protocol.AgentCard
}

func (a *CodeAnalyzer) Card() *protocol.AgentCard { return a.card }

func (a *CodeAnalyzer) HandleMessage(_ context.Context, params *protocol.SendMessageParams) (*protocol.Task, *protocol.Message, error) {
	code := params.Message.TextContent()
	if strings.TrimSpace(code) == "" {
		return nil, nil, fmt.Errorf("no code provided — send source code as a text message")
	}

	report := analyze(code)

	trueBool := true
	task := &protocol.Task{
		Status: protocol.TaskStatus{State: protocol.TaskStateCompleted},
		Artifacts: []protocol.Artifact{
			{
				Name:      "analysis.md",
				Parts:     []protocol.Part{{Kind: "text", Text: report}},
				LastChunk: &trueBool,
			},
		},
	}
	return task, nil, nil
}

// ─── Analysis Engine ────────────────────────────────────────────────────────

type analysisResult struct {
	Language    string
	TotalLines int
	CodeLines  int
	BlankLines int
	CommentLines int
	Functions  []string
	Imports    []string
	Todos      []todoItem
	Complexity string
}

type todoItem struct {
	Line    int
	Kind    string // TODO, FIXME, HACK, XXX
	Text    string
}

func analyze(code string) string {
	lines := strings.Split(code, "\n")
	lang := detectLanguage(code)
	result := analysisResult{
		Language:   lang,
		TotalLines: len(lines),
	}

	commentPrefix, blockStart, blockEnd := commentSyntax(lang)
	inBlockComment := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Blank lines
		if trimmed == "" {
			result.BlankLines++
			continue
		}

		// Block comments
		if blockStart != "" && strings.Contains(trimmed, blockStart) {
			inBlockComment = true
		}
		if inBlockComment {
			result.CommentLines++
			if blockEnd != "" && strings.Contains(trimmed, blockEnd) {
				inBlockComment = false
			}
			continue
		}

		// Single-line comments
		if commentPrefix != "" && strings.HasPrefix(trimmed, commentPrefix) {
			result.CommentLines++
		} else {
			result.CodeLines++
		}

		// TODO/FIXME/HACK detection
		for _, keyword := range []string{"TODO", "FIXME", "HACK", "XXX"} {
			if idx := strings.Index(strings.ToUpper(trimmed), keyword); idx >= 0 {
				result.Todos = append(result.Todos, todoItem{
					Line: i + 1,
					Kind: keyword,
					Text: strings.TrimSpace(trimmed),
				})
			}
		}
	}

	// Functions
	result.Functions = detectFunctions(code, lang)

	// Imports
	result.Imports = detectImports(code, lang)

	// Complexity
	result.Complexity = assessComplexity(code, lang, result.CodeLines, len(result.Functions))

	return formatReport(result)
}

func detectLanguage(code string) string {
	switch {
	case strings.Contains(code, "package main") || strings.Contains(code, "func "):
		return "Go"
	case strings.Contains(code, "def ") && strings.Contains(code, ":"):
		if strings.Contains(code, "import ") || strings.Contains(code, "from ") {
			return "Python"
		}
		return "Python"
	case strings.Contains(code, "fn ") && strings.Contains(code, "->"):
		return "Rust"
	case strings.Contains(code, "interface ") && strings.Contains(code, ": "):
		if strings.Contains(code, "import ") {
			return "TypeScript"
		}
	case strings.Contains(code, "const ") || strings.Contains(code, "function ") || strings.Contains(code, "=>"):
		if strings.Contains(code, ": string") || strings.Contains(code, ": number") || strings.Contains(code, "interface ") {
			return "TypeScript"
		}
		return "JavaScript"
	case strings.Contains(code, "public class ") || strings.Contains(code, "public static void main"):
		return "Java"
	case strings.Contains(code, "#include"):
		return "C/C++"
	}
	return "Unknown"
}

func commentSyntax(lang string) (lineComment, blockStart, blockEnd string) {
	switch lang {
	case "Go", "JavaScript", "TypeScript", "Java", "Rust", "C/C++":
		return "//", "/*", "*/"
	case "Python":
		return "#", `"""`, `"""`
	default:
		return "//", "/*", "*/"
	}
}

var funcPatterns = map[string]*regexp.Regexp{
	"Go":         regexp.MustCompile(`func\s+(\([\w\s*]+\)\s+)?(\w+)\s*\(`),
	"Python":     regexp.MustCompile(`def\s+(\w+)\s*\(`),
	"JavaScript": regexp.MustCompile(`(?:function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function|\())`),
	"TypeScript": regexp.MustCompile(`(?:function\s+(\w+)|(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function|\())`),
	"Rust":       regexp.MustCompile(`fn\s+(\w+)\s*[(<]`),
	"Java":       regexp.MustCompile(`(?:public|private|protected|static|\s)+[\w<>\[\]]+\s+(\w+)\s*\(`),
	"C/C++":      regexp.MustCompile(`[\w*]+\s+(\w+)\s*\([^)]*\)\s*\{`),
}

func detectFunctions(code, lang string) []string {
	pat, ok := funcPatterns[lang]
	if !ok {
		return nil
	}
	matches := pat.FindAllStringSubmatch(code, -1)
	var names []string
	seen := map[string]bool{}
	for _, m := range matches {
		for i := len(m) - 1; i >= 1; i-- {
			if m[i] != "" && !seen[m[i]] {
				seen[m[i]] = true
				names = append(names, m[i])
				break
			}
		}
	}
	return names
}

var importPatterns = map[string]*regexp.Regexp{
	"Go":         regexp.MustCompile(`"([^"]+)"`),
	"Python":     regexp.MustCompile(`(?:from\s+([\w.]+)\s+import|import\s+([\w.]+))`),
	"JavaScript": regexp.MustCompile(`(?:require\(['"]([^'"]+)['"]\)|from\s+['"]([^'"]+)['"])`),
	"TypeScript": regexp.MustCompile(`(?:require\(['"]([^'"]+)['"]\)|from\s+['"]([^'"]+)['"])`),
	"Rust":       regexp.MustCompile(`use\s+([\w:]+)`),
	"Java":       regexp.MustCompile(`import\s+([\w.]+);`),
}

func detectImports(code, lang string) []string {
	pat, ok := importPatterns[lang]
	if !ok {
		return nil
	}

	// For Go, only scan the import block
	scanCode := code
	if lang == "Go" {
		if idx := strings.Index(code, "import ("); idx >= 0 {
			end := strings.Index(code[idx:], ")")
			if end >= 0 {
				scanCode = code[idx : idx+end]
			}
		}
	}

	matches := pat.FindAllStringSubmatch(scanCode, -1)
	var imports []string
	seen := map[string]bool{}
	for _, m := range matches {
		for i := 1; i < len(m); i++ {
			if m[i] != "" && !seen[m[i]] {
				seen[m[i]] = true
				imports = append(imports, m[i])
				break
			}
		}
	}
	return imports
}

func assessComplexity(code, lang string, codeLines, funcCount int) string {
	// Count nesting indicators
	nestingKeywords := 0
	for _, kw := range []string{"if ", "for ", "switch ", "select ", "while ", "match ", "case "} {
		nestingKeywords += strings.Count(code, kw)
	}

	// Rough cyclomatic complexity estimate
	score := nestingKeywords
	if funcCount > 0 {
		score = score / funcCount // average per function
	}

	switch {
	case score <= 2:
		return "Low"
	case score <= 5:
		return "Moderate"
	case score <= 10:
		return "High"
	default:
		return "Very High"
	}
}

func formatReport(r analysisResult) string {
	var sb strings.Builder

	sb.WriteString("# Code Analysis Report\n\n")
	sb.WriteString(fmt.Sprintf("**Language:** %s\n\n", r.Language))

	// Line counts
	sb.WriteString("## Line Counts\n\n")
	sb.WriteString("| Metric | Count |\n")
	sb.WriteString("|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Total lines | %d |\n", r.TotalLines))
	sb.WriteString(fmt.Sprintf("| Code lines | %d |\n", r.CodeLines))
	sb.WriteString(fmt.Sprintf("| Comment lines | %d |\n", r.CommentLines))
	sb.WriteString(fmt.Sprintf("| Blank lines | %d |\n", r.BlankLines))
	if r.TotalLines > 0 {
		commentRatio := float64(r.CommentLines) / float64(r.TotalLines) * 100
		sb.WriteString(fmt.Sprintf("| Comment ratio | %.1f%% |\n", commentRatio))
	}

	// Functions
	sb.WriteString(fmt.Sprintf("\n## Functions (%d found)\n\n", len(r.Functions)))
	if len(r.Functions) > 0 {
		for _, f := range r.Functions {
			sb.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
	} else {
		sb.WriteString("No functions detected.\n")
	}

	// Imports
	sb.WriteString(fmt.Sprintf("\n## Dependencies (%d found)\n\n", len(r.Imports)))
	if len(r.Imports) > 0 {
		for _, imp := range r.Imports {
			sb.WriteString(fmt.Sprintf("- `%s`\n", imp))
		}
	} else {
		sb.WriteString("No imports detected.\n")
	}

	// TODOs
	if len(r.Todos) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Annotations (%d found)\n\n", len(r.Todos)))
		for _, t := range r.Todos {
			sb.WriteString(fmt.Sprintf("- **%s** (line %d): `%s`\n", t.Kind, t.Line, t.Text))
		}
	}

	// Complexity
	sb.WriteString("\n## Complexity\n\n")
	sb.WriteString(fmt.Sprintf("**Overall complexity:** %s\n", r.Complexity))
	if len(r.Functions) > 0 && r.CodeLines > 0 {
		avgLen := r.CodeLines / len(r.Functions)
		sb.WriteString(fmt.Sprintf("**Average function length:** ~%d lines\n", avgLen))
	}

	sb.WriteString("\n---\n*Analyzed by Code Analyzer Agent · AgentExchange*\n")
	return sb.String()
}

// ─── Helpers ────────────────────────────────────────────────────────────────

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
