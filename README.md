# FAXP — Fixed Agent eXchange Protocol

> **Stripe Connect for AI agents.** Publish your agent once. Any organization can discover and call it through the platform — with authentication, metering, and real-time observability included.

FAXP is an open protocol and hosted platform for cross-organization AI agent communication. It extends [Google's A2A Protocol](https://a2a-protocol.org) with economic primitives (pricing hints, SLA negotiation), cryptographic message signing, and a platform routing layer that makes connecting agents across companies as simple as a single API call.

---

## Why FAXP?

| Problem | Without FAXP | With FAXP |
|---|---|---|
| Cross-company agent calls | Custom per-integration HTTP | One API key, one endpoint |
| Capability discovery | Email/Slack + docs | `GET /platform/v1/agents?skill=summarize` |
| Auth between orgs | Manual key exchange | Platform-managed, per-org keys |
| Billing | Invoice at month end | Per-call metering, automatic |
| Observability | Nothing | Live call feed, latency, spend |

---

## Protocol Design

FAXP is a strict superset of [A2A v1.0.0](https://a2a-protocol.org/dev/specification/). Any A2A-compatible agent is a valid FAXP agent. Extensions are additive and gracefully ignored by A2A clients.

See [SPEC.md](./SPEC.md) for the full protocol specification.

---

## Quick Start

### Prerequisites
- Go 1.21+
- `make`

### Run the demo (3 companies, live dashboard)

```bash
# Terminal 1 — start the platform
make platform

# Terminal 2 — start Company B's writer agent (registers with platform on start)
make writer

# Terminal 3 — start Company C's analyst agent
make analyst

# Terminal 4 — run Company A's researcher (discovers + calls the others)
make researcher

# Open the dashboard
open http://localhost:8080
```

You'll see Company A's researcher:
1. Query the platform registry for agents with `report_generation` capability
2. Route a call to Company B's writer through the platform
3. Receive a streaming report response
4. Route the report to Company C's analyst
5. Display the final structured analysis

All calls appear in the live dashboard with latency, cost, and organization attribution.

![Dashboard view after demo](/examples/FAXP-demo.png)

---

## SDK Usage

### Register your agent

```go
import (
    "github.com/zigamedved/faxp/pkg/protocol"
    "github.com/zigamedved/faxp/pkg/transport/http"
    "github.com/zigamedved/faxp/pkg/platform"
)

// Define your agent
card := &protocol.AgentCard{
    Name:        "My Agent",
    Description: "Does useful things.",
    URL:         "http://localhost:9000",
    Version:     "1.0.0",
    Skills: []protocol.Skill{{
        ID:   "my_skill",
        Name: "My Skill",
        Tags: []string{"useful"},
    }},
    Capabilities: protocol.AgentCapabilities{Streaming: true},
    FaxpPricing: &protocol.Pricing{
        Model:      "per-call",
        PerCallUSD: 0.001,
    },
}

agent := &MyAgent{card: card}
server := faxphttp.NewServer(agent)

// Register with the platform
client := platform.NewClient("http://localhost:8080", "your-api-key")
client.Register(ctx, "my-agent", "http://localhost:9000", card)

// Start serving
http.ListenAndServe(":9000", server)
```

### Call another agent

```go
// Discover agents by skill
agents, _ := client.FindAgents(ctx, "report_generation")

// Route a call through the platform
result, _ := client.RouteMessage(ctx, agents[0].ID, &protocol.SendMessageParams{
    Message: protocol.Message{
        Role:  "user",
        Parts: []protocol.Part{{Kind: "text", Text: "Write a report on AI trends."}},
    },
})
```

---

## Project Structure

```
agent-FIX/
├── SPEC.md                      Protocol specification
├── DEMO.md                      Demo walkthrough
├── pkg/
│   ├── protocol/                Core A2A-compatible types + FAXP extensions
│   ├── identity/                Ed25519 key management and message signing
│   ├── transport/http/          HTTP server, client, and SSE support
│   ├── registry/                In-memory agent registry
│   └── platform/                Platform routing, auth, and metering
├── cmd/
│   ├── platform/                Platform server binary
│   └── fixctl/                  CLI for managing agents and sending messages
└── examples/
    ├── company-a-researcher/    Demo: research agent (discovers + calls others)
    ├── company-b-writer/        Demo: writing agent (streams reports)
    └── company-c-analyst/       Demo: analysis agent (returns structured metrics)
```

---

## A2A Compatibility

FAXP implements the full A2A v1.0.0 protocol binding:

- ✅ Agent Cards at `/.well-known/agent.json`
- ✅ `message/send` JSON-RPC method
- ✅ `message/stream` with SSE
- ✅ `tasks/get`, `tasks/cancel`
- ✅ Standard A2A error codes

FAXP adds (ignored by A2A clients):
- `x-faxp-pricing` in Agent Cards
- `x-faxp-pubkey` in Agent Cards
- `x-faxp-sig` in message metadata
- `quote/request`, `quote/accept` methods
- Platform routing API

---

## License

MIT — see [LICENSE](./LICENSE)
