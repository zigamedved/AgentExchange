package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/zigamedved/agent-exchange/pkg/platform"
)

func main() {
	addr := os.Getenv("AX_PLATFORM_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// Configure log level via LOG_LEVEL env var (debug, info, warn, error).
	logLevel := slog.LevelInfo
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	// Create platform with demo configuration
	p := platform.New(
		platform.WithRegistration(platform.RegistrationOpen),
		platform.WithDefaultCredits(1000),
	)

	// Seed demo organizations
	if auth, ok := p.Auth().(*platform.MemoryAuth); ok {
		auth.Seed("org-a", "Company A (Researcher)", "ax_companya_demo", platform.OrgPrivate, 1000)
		auth.Seed("org-b", "Company B (Writer)", "ax_companyb_demo", platform.OrgPublic, 1000)
		auth.Seed("org-c", "Company C (Analyst)", "ax_companyc_demo", platform.OrgPublic, 1000)
		auth.Seed("org-admin", "Platform Admin", "ax_admin_demo", platform.OrgPublic, 10000)
		auth.Seed("cli-client", "CLI Client", "api-key", platform.OrgPrivate, 1000)
	}

	slog.Info("AgentExchange platform starting",
		"addr", addr,
		"dashboard", "http://localhost"+addr,
	)
	fmt.Printf("\n  AX  AgentExchange\n")
	fmt.Printf("  Dashboard  >  http://localhost%s\n", addr)
	fmt.Printf("  Registry   >  http://localhost%s/platform/v1/agents\n", addr)
	fmt.Printf("  Register   >  POST http://localhost%s/platform/v1/orgs\n", addr)
	fmt.Printf("\n  Demo API keys (1000 credits each):\n")
	fmt.Printf("    Company A (private) >  ax_companya_demo\n")
	fmt.Printf("    Company B (public)  >  ax_companyb_demo\n")
	fmt.Printf("    Company C (public)  >  ax_companyc_demo\n\n")

	srv := &http.Server{
		Addr:    addr,
		Handler: p.Handler(),
	}

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("platform server error", "err", err)
		os.Exit(1)
	}
}
