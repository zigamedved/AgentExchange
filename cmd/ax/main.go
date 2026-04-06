package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/urfave/cli/v2"
	"github.com/zigamedved/agent-exchange/pkg/identity"
	"github.com/zigamedved/agent-exchange/pkg/platform"
	"github.com/zigamedved/agent-exchange/pkg/protocol"
	"github.com/zigamedved/agent-exchange/pkg/registry"
	axhttp "github.com/zigamedved/agent-exchange/pkg/transport/http"
)

func main() {
	app := &cli.App{
		Name:  "ax",
		Usage: "AgentExchange command-line tool",
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Start the AgentExchange platform server",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "addr", Value: ":8080", EnvVars: []string{"AX_PLATFORM_ADDR"}, Usage: "Listen address"},
				},
				Action: func(c *cli.Context) error {
					addr := c.String("addr")

					// Persistent stores
					store, err := registry.NewSQLiteStore("ax.db")
					if err != nil {
						return fmt.Errorf("open registry db: %w", err)
					}
					defer store.Close()

					auth, err := platform.NewSQLiteAuth("ax.db", 1000)
					if err != nil {
						return fmt.Errorf("open auth db: %w", err)
					}
					defer auth.Close()

					// Seed demo organizations
					auth.Seed("org-a", "Company A (Researcher)", "ax_companya_demo", platform.OrgPrivate, 1000)
					auth.Seed("org-b", "Company B (Writer)", "ax_companyb_demo", platform.OrgPublic, 1000)
					auth.Seed("org-c", "Company C (Analyst)", "ax_companyc_demo", platform.OrgPublic, 1000)
					auth.Seed("org-admin", "Platform Admin", "ax_admin_demo", platform.OrgPublic, 10000)
					auth.Seed("cli-client", "CLI Client", "api-key", platform.OrgPrivate, 1000)

					p := platform.New(
						platform.WithStore(store),
						platform.WithAuth(auth),
						platform.WithRegistration(platform.RegistrationOpen),
						platform.WithDefaultCredits(1000),
					)

					fmt.Printf("\n  AX  AgentExchange\n")
					fmt.Printf("  Dashboard  >  http://localhost%s\n", addr)
					fmt.Printf("  Registry   >  http://localhost%s/platform/v1/agents\n", addr)
					fmt.Printf("  Register   >  POST http://localhost%s/platform/v1/orgs\n", addr)
					fmt.Printf("\n  Demo API keys (1000 credits each):\n")
					fmt.Printf("    Company A (private) >  ax_companya_demo\n")
					fmt.Printf("    Company B (public)  >  ax_companyb_demo\n")
					fmt.Printf("    Company C (public)  >  ax_companyc_demo\n\n")

					srv := &http.Server{Addr: addr, Handler: p.Handler()}
					slog.Info("platform starting", "addr", addr)
					return srv.ListenAndServe()
				},
			},
			{
				Name:  "keys",
				Usage: "Manage Ed25519 identity keys",
				Subcommands: []*cli.Command{
					{
						Name:  "generate",
						Usage: "Generate a new Ed25519 key pair",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "out", Value: "ax.key", Usage: "Output file path"},
						},
						Action: func(c *cli.Context) error {
							id, err := identity.New()
							if err != nil {
								return err
							}
							if err := id.SaveToFile(c.String("out")); err != nil {
								return err
							}
							fmt.Printf("Public key:  %s\n", id.PublicKeyBase64())
							fmt.Printf("Key saved:   %s\n", c.String("out"))
							return nil
						},
					},
					{
						Name:  "show",
						Usage: "Print the public key from a key file",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "key", Value: "ax.key", Usage: "Key file path"},
						},
						Action: func(c *cli.Context) error {
							id, err := identity.LoadFromFile(c.String("key"))
							if err != nil {
								return err
							}
							fmt.Printf("Public key: %s\n", id.PublicKeyBase64())
							return nil
						},
					},
				},
			},
			{
				Name:  "discover",
				Usage: "Discover agents in the registry",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "platform", Value: "http://localhost:8080", Usage: "Platform URL"},
					&cli.StringFlag{Name: "api-key", EnvVars: []string{"AX_API_KEY"}, Usage: "Platform API key"},
					&cli.StringFlag{Name: "skill", Usage: "Filter by skill ID or tag"},
				},
				Action: func(c *cli.Context) error {
					client := platform.NewPlatformClient(c.String("platform"), c.String("api-key"))
					agents, err := client.FindAgents(context.Background(), c.String("skill"))
					if err != nil {
						return err
					}
					if len(agents) == 0 {
						fmt.Println("No agents found.")
						return nil
					}
					fmt.Printf("%-36s  %-24s  %-16s  %s\n", "ID", "Name", "Org", "Endpoint")
					fmt.Println("─────────────────────────────────────────────────────────────────────────────────────")
					for _, a := range agents {
						fmt.Printf("%-36s  %-24s  %-16s  %s\n",
							a.ID, a.Name, a.Organization, a.EndpointURL)
					}
					return nil
				},
			},
			{
				Name:  "call",
				Usage: "Send a message to an agent",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "to", Required: true, Usage: "Agent endpoint URL or platform route URL"},
					&cli.StringFlag{Name: "text", Usage: "Text message content"},
					&cli.StringFlag{Name: "api-key", EnvVars: []string{"AX_API_KEY"}, Usage: "API key"},
					&cli.BoolFlag{Name: "stream", Usage: "Use streaming (a2a_sendStreamingMessage)"},
				},
				Action: func(c *cli.Context) error {
					client := axhttp.NewClient(c.String("to"), c.String("api-key"))
					text := c.String("text")
					if text == "" && c.Args().Len() > 0 {
						text = c.Args().First()
					}
					if text == "" {
						return fmt.Errorf("provide --text or a positional argument")
					}

					params := &protocol.SendMessageParams{
						Message: protocol.NewTextMessage(text),
					}

					if c.Bool("stream") {
						fmt.Printf("Streaming response from %s:\n\n", c.String("to"))
						return client.StreamMessage(context.Background(), params, func(event map[string]any) error {
							if event["kind"] == "artifact" {
								if artifact, ok := event["artifact"].(map[string]any); ok {
									if parts, ok := artifact["parts"].([]any); ok {
										for _, p := range parts {
											if part, ok := p.(map[string]any); ok {
												if part["kind"] == "text" {
													fmt.Print(part["text"])
												}
											}
										}
									}
								}
							}
							if event["kind"] == "status" && event["final"] == true {
								fmt.Println()
							}
							return nil
						})
					}

					task, msg, err := client.SendMessage(context.Background(), params)
					if err != nil {
						return err
					}

					if task != nil {
						enc := json.NewEncoder(os.Stdout)
						enc.SetIndent("", "  ")
						return enc.Encode(task)
					}
					if msg != nil {
						fmt.Println(msg.TextContent())
					}
					return nil
				},
			},
			{
				Name:  "card",
				Usage: "Fetch an agent's card",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "url", Required: true, Usage: "Agent base URL"},
				},
				Action: func(c *cli.Context) error {
					client := axhttp.NewClient(c.String("url"), "")
					card, err := client.GetCard(context.Background())
					if err != nil {
						return err
					}
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(card)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("ax error", "err", err)
		os.Exit(1)
	}
}
