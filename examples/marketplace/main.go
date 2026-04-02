// Example: Public marketplace with invite-gated registration and credits.
//
// This shows how to configure AX as a public agent marketplace where:
// - New orgs require an invite code to register
// - Each org gets 50 free credits
// - All calls are metered and logged
// - Orgs, agents, and invites persist in SQLite
//
// Usage:
//
//	go run ./examples/marketplace
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
	dbPath := envOr("AX_DB", "ax-marketplace.db")

	// Persistent stores
	store, err := registry.NewSQLiteStore(dbPath)
	if err != nil {
		slog.Error("failed to open registry database", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	auth, err := platform.NewSQLiteAuth(dbPath, 50)
	if err != nil {
		slog.Error("failed to open auth database", "err", err)
		os.Exit(1)
	}
	defer auth.Close()

	invites, err := platform.NewSQLiteInviteStore(dbPath)
	if err != nil {
		slog.Error("failed to open invite database", "err", err)
		os.Exit(1)
	}
	defer invites.Close()

	// Seed demo orgs
	auth.Seed("demo-publisher", "Demo Publisher", "ax_demo_pub", platform.OrgPublic, 1000)
	auth.Seed("demo-consumer", "Demo Consumer", "ax_demo_con", platform.OrgPrivate, 100)

	// Platform with marketplace config
	p := platform.New(
		platform.WithStore(store),
		platform.WithAuth(auth),
		platform.WithRegistration(platform.RegistrationInvite),
		platform.WithInviteStore(invites),
		platform.WithDefaultCredits(50),
		platform.WithOnCall(func(rec *platform.CallRecord) {
			slog.Info("call completed",
				"caller", rec.CallerOrg,
				"agent", rec.AgentName,
				"method", rec.Method,
				"price", rec.PriceUSD,
				"latency_ms", rec.LatencyMS,
			)
		}),
	)

	// Generate a few invite codes for testing
	fmt.Printf("\n  AX  Marketplace\n")
	fmt.Printf("  Dashboard  >  http://localhost%s?api_key=ax_demo_pub\n", addr)
	fmt.Printf("  Database   >  %s\n", dbPath)
	fmt.Printf("  Mode       >  invite-only registration\n")
	fmt.Printf("  Credits    >  50 per new org\n\n")
	fmt.Printf("  Invite codes:\n")
	for i := 0; i < 5; i++ {
		code, _ := invites.Create("admin")
		fmt.Printf("    %s\n", code)
	}
	fmt.Printf("\n  Demo keys:\n")
	fmt.Printf("    Publisher >  ax_demo_pub\n")
	fmt.Printf("    Consumer >  ax_demo_con\n\n")

	srv := &http.Server{Addr: addr, Handler: p.Handler()}
	slog.Info("marketplace starting", "addr", addr)
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
