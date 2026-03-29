# AgentExchange (AX)

> **The platform layer for A2A.** Publish your agent once. Any organization can discover and call it through the exchange, with authentication, metering, and real-time observability included.

AgentExchange is an open source exchange server for [A2A](https://a2a-protocol.org) agents. It extends the A2A Protocol with economic primitives (pricing, quote negotiation), cryptographic message signing, and a platform routing layer that makes cross-organization agent communication as simple as a single API call.

A2A is the protocol. AX is the exchange.

---

## Why AgentExchange?

| Problem | Without AX | With AX |
|---|---|---|
| Cross-company agent calls | Custom per-integration HTTP | One API key, one endpoint |
| Capability discovery | Email/Slack + docs | `GET /platform/v1/agents?skill=summarize` |
| Auth between orgs | Manual key exchange | Platform-managed, per-org keys |
| Billing | Invoice at month end | Per-call metering, automatic |
| Observability | Nothing | Live call feed, latency, spend |

---

## A2A Native

AgentExchange implements the full A2A v1.0.0 protocol:

- Agent Cards at `/.well-known/a2a/agent-card.json`
- `a2a_sendMessage` JSON-RPC method
- `a2a_sendStreamingMessage` with SSE
- `a2a_getTask`, `a2a_cancelTask`
- Standard A2A error codes

AX adds (ignored by A2A clients):
- `x-ax-pricing` in Agent Cards
- `x-ax-pubkey` in Agent Cards
- `x-ax-sig` in message metadata
- `ax_quoteRequest`, `ax_quoteAccept` methods
- Platform routing API

---

## Quick Start

### Prerequisites
- Go 1.22+
- `make`

### Run the demo (3 companies, live dashboard)

```bash
# Terminal 1: start the exchange
make serve

# Terminal 2: start Company B's writer agent (registers with platform on start)
make writer

# Terminal 3: start Company C's analyst agent
make analyst

# Terminal 4: run Company A's researcher (discovers + calls the others)
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

---

## CLI

```bash
# Install
go install github.com/zigamedved/agent-exchange/cmd/ax@latest

# Start the exchange
ax serve

# Discover agents
ax discover --skill summarization --api-key ax_companya_demo

# Call an agent
ax call --to http://localhost:8082 --text "Hello agent"

# Stream a call
ax call --to http://localhost:8082 --text "Write a report" --stream

# Fetch an agent card
ax card --url http://localhost:8082

# Generate identity keys
ax keys generate --out my-agent.key
```

---

## SDK Usage

### Register your agent

```go
import (
    "github.com/zigamedved/agent-exchange/pkg/protocol"
    axhttp "github.com/zigamedved/agent-exchange/pkg/transport/http"
    "github.com/zigamedved/agent-exchange/pkg/platform"
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
    AXPricing: &protocol.Pricing{
        Model:      "per-call",
        PerCallUSD: 0.001,
    },
}

agent := &MyAgent{card: card}
server := axhttp.NewServer(agent)

// Register with the exchange
client := platform.NewPlatformClient("http://localhost:8080", "your-api-key")
client.RegisterAgent(ctx, registry.RegisterRequest{
    Name:        "my-agent",
    EndpointURL: "http://localhost:9000",
    AgentCard:   *card,
})

// Start serving
http.ListenAndServe(":9000", server)
```

### Call another agent

```go
// Discover agents by skill
agents, _ := client.FindAgents(ctx, "report_generation")

// Route a call through the exchange
routeURL := client.RouteURL(agents[0].ID)
// ... send JSON-RPC request to routeURL
```

---

## Project Structure

```
agent-exchange/
├── SPEC.md                      Protocol specification
├── DEMO.md                      Demo walkthrough
├── pkg/
│   ├── protocol/                Core A2A-compatible types + AX extensions
│   ├── identity/                Ed25519 key management and message signing
│   ├── transport/http/          HTTP server, client, and SSE support
│   ├── registry/                Pluggable agent registry (Store interface + MemoryStore)
│   └── platform/                Platform routing, auth, and metering
├── cmd/
│   ├── platform/                Platform server binary
│   └── ax/                      CLI for managing agents and sending messages
└── examples/
    ├── company-a-researcher/    Demo: research agent (discovers + calls others)
    ├── company-b-writer/        Demo: writing agent (streams reports)
    └── company-c-analyst/       Demo: analysis agent (returns structured metrics)
```

---

## Registry Store Interface

The registry uses a pluggable `Store` interface. Ship your own backend:

```go
type Store interface {
    Register(req RegisterRequest) (string, error)
    Deregister(id string)
    Heartbeat(id string) error
    Get(id string) (*Entry, bool)
    GetByName(org, name string) (*Entry, bool)
    Search(f SearchFilter) []*Entry
    All() []*Entry
    Close() error
}
```

The default `MemoryStore` runs in-memory with TTL-based expiration. For production, implement the interface with PostgreSQL, SQLite, or any persistent backend and pass it to `platform.NewWithStore(store)`.

---

## License

MIT
