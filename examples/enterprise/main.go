// Example: Enterprise internal agent exchange with closed registration.
//
// This shows how to configure AX for internal company use where:
// - Only admins can create orgs (closed registration)
// - No credits/billing (intra-company, free for all)
// - Each team gets an org with their agents
// - Calls are tracked for observability, not billing
// - Data persists in SQLite
//
// Usage:
//
//	go run ./examples/enterprise
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/zigamedved/agent-exchange/pkg/platform"
	"github.com/zigamedved/agent-exchange/pkg/registry"
)

func main() {
	addr := envOr("AX_ADDR", ":8080")
	dbPath := envOr("AX_DB", "ax-enterprise.db")

	// Persistent stores
	store, err := registry.NewSQLiteStore(dbPath)
	if err != nil {
		slog.Error("failed to open registry database", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	auth, err := platform.NewSQLiteAuth(dbPath, 0)
	if err != nil {
		slog.Error("failed to open auth database", "err", err)
		os.Exit(1)
	}
	defer auth.Close()

	// Pre-create team orgs
	auth.Seed("team-ml", "ML Team", "ax_ml_team", platform.OrgPublic, 0)
	auth.Seed("team-backend", "Backend Team", "ax_backend_team", platform.OrgPublic, 0)
	auth.Seed("team-data", "Data Team", "ax_data_team", platform.OrgPublic, 0)
	auth.Seed("team-platform", "Platform Team", "ax_platform_admin", platform.OrgPublic, 0)

	// Platform with enterprise config
	p := platform.New(
		platform.WithStore(store),
		platform.WithAuth(auth),
		platform.WithRegistration(platform.RegistrationClosed),
		platform.WithOnCall(func(rec *platform.CallRecord) {
			slog.Info("agent call",
				"from_team", rec.CallerOrg,
				"to_agent", rec.AgentName,
				"agent_team", rec.AgentOrg,
				"method", rec.Method,
				"latency_ms", rec.LatencyMS,
				"status", rec.Status,
			)
		}),
	)

	fmt.Printf("\n  AX  Enterprise Agent Exchange\n")
	fmt.Printf("  Dashboard  >  http://localhost%s?api_key=ax_platform_admin\n", addr)
	fmt.Printf("  Database   >  %s\n", dbPath)
	fmt.Printf("  Mode       >  closed (admin-only registration)\n\n")
	fmt.Printf("  Team API keys:\n")
	fmt.Printf("    ML Team       >  ax_ml_team\n")
	fmt.Printf("    Backend Team  >  ax_backend_team\n")
	fmt.Printf("    Data Team     >  ax_data_team\n")
	fmt.Printf("    Platform      >  ax_platform_admin\n\n")

	srv := &http.Server{Addr: addr, Handler: p.Handler()}
	slog.Info("enterprise exchange starting", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
