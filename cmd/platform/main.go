package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/zigamedved/agent-exchange/pkg/platform"
)

func main() {
	addr := os.Getenv("AX_PLATFORM_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	p := platform.New()

	slog.Info("AgentExchange platform starting",
		"addr", addr,
		"dashboard", "http://localhost"+addr,
	)
	fmt.Printf("\n  AX  AgentExchange\n")
	fmt.Printf("  Dashboard  >  http://localhost%s\n", addr)
	fmt.Printf("  Registry   >  http://localhost%s/platform/v1/agents\n", addr)
	fmt.Printf("\n  Demo API keys:\n")
	fmt.Printf("    Company A  >  ax_companya_demo\n")
	fmt.Printf("    Company B  >  ax_companyb_demo\n")
	fmt.Printf("    Company C  >  ax_companyc_demo\n\n")

	srv := &http.Server{
		Addr:    addr,
		Handler: p.Handler(),
	}

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("platform server error", "err", err)
		os.Exit(1)
	}
}
