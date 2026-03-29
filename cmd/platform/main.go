package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/zigamedved/faxp/pkg/platform"
)

func main() {
	addr := os.Getenv("FAXP_PLATFORM_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	p := platform.New()

	slog.Info("FAXP platform starting",
		"addr", addr,
		"dashboard", "http://localhost"+addr,
	)
	fmt.Printf("\n  ⬡  FAXP Platform\n")
	fmt.Printf("  Dashboard  →  http://localhost%s\n", addr)
	fmt.Printf("  Registry   →  http://localhost%s/platform/v1/agents\n", addr)
	fmt.Printf("\n  Demo API keys:\n")
	fmt.Printf("    Company A  →  faxp_companya_demo\n")
	fmt.Printf("    Company B  →  faxp_companyb_demo\n")
	fmt.Printf("    Company C  →  faxp_companyc_demo\n\n")

	srv := &http.Server{
		Addr:    addr,
		Handler: p.Handler(),
	}

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("platform server error", "err", err)
		os.Exit(1)
	}
}
