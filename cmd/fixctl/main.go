package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/urfave/cli/v2"
	"github.com/zigamedved/faxp/pkg/identity"
	"github.com/zigamedved/faxp/pkg/platform"
	"github.com/zigamedved/faxp/pkg/protocol"
	faxphttp "github.com/zigamedved/faxp/pkg/transport/http"
)

func main() {
	app := &cli.App{
		Name:  "fixctl",
		Usage: "FAXP command-line tool",
		Commands: []*cli.Command{
			{
				Name:  "keys",
				Usage: "Manage Ed25519 identity keys",
				Subcommands: []*cli.Command{
					{
						Name:  "generate",
						Usage: "Generate a new Ed25519 key pair",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "out", Value: "faxp.key", Usage: "Output file path"},
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
							&cli.StringFlag{Name: "key", Value: "faxp.key", Usage: "Key file path"},
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
				Name:  "registry",
				Usage: "Manage agents in the FAXP registry",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "platform", Value: "http://localhost:8080", Usage: "Platform URL"},
					&cli.StringFlag{Name: "api-key", EnvVars: []string{"FAXP_API_KEY"}, Usage: "Platform API key"},
				},
				Subcommands: []*cli.Command{
					{
						Name:  "ls",
						Usage: "List registered agents",
						Action: func(c *cli.Context) error {
							client := platform.NewPlatformClient(c.String("platform"), c.String("api-key"))
							agents, err := client.FindAgents(context.Background(), "")
							if err != nil {
								return err
							}
							if len(agents) == 0 {
								fmt.Println("No agents registered.")
								return nil
							}
							fmt.Printf("%-36s  %-24s  %-16s  %s\n", "ID", "Name", "Org", "Endpoint")
							fmt.Printf("%s\n", "────────────────────────────────────────────────────────────────────────────────────")
							for _, a := range agents {
								fmt.Printf("%-36s  %-24s  %-16s  %s\n",
									a.ID, a.Name, a.Organization, a.EndpointURL)
							}
							return nil
						},
					},
					{
						Name:  "find",
						Usage: "Find agents by skill",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "skill", Required: true, Usage: "Skill ID or tag to search for"},
						},
						Action: func(c *cli.Context) error {
							client := platform.NewPlatformClient(c.String("platform"), c.String("api-key"))
							agents, err := client.FindAgents(context.Background(), c.String("skill"))
							if err != nil {
								return err
							}
							if len(agents) == 0 {
								fmt.Printf("No agents found with skill: %s\n", c.String("skill"))
								return nil
							}
							for _, a := range agents {
								pricing := ""
								if p := a.AgentCard.FaxpPricing; p != nil {
									pricing = fmt.Sprintf("  $%.4f/call", p.PerCallUSD)
								}
								fmt.Printf("  %s  (%s)%s\n", a.Name, a.ID[:8], pricing)
							}
							return nil
						},
					},
				},
			},
			{
				Name:  "send",
				Usage: "Send a message to an agent",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "to", Required: true, Usage: "Agent endpoint URL or platform route URL"},
					&cli.StringFlag{Name: "text", Usage: "Text message content"},
					&cli.StringFlag{Name: "api-key", EnvVars: []string{"FAXP_API_KEY"}, Usage: "API key"},
					&cli.BoolFlag{Name: "stream", Usage: "Use streaming (message/stream)"},
				},
				Action: func(c *cli.Context) error {
					client := faxphttp.NewClient(c.String("to"), c.String("api-key"))
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
					client := faxphttp.NewClient(c.String("url"), "")
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
		slog.Error("fixctl error", "err", err)
		os.Exit(1)
	}
}
