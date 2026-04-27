# AgentExchange (AX)

> **The framework for A2A agent exchanges.** Build a marketplace, an internal agent platform, or anything in between.

AgentExchange is an open-source Go framework for building [A2A](https://a2a-protocol.org) agent exchanges. It provides agent discovery, call routing, authentication, metering, and real-time observability — all configurable and extensible.

A2A is the protocol. AX is the exchange.

[![Demo](https://img.shields.io/badge/Watch-Demo-black)](https://github.com/YOUR/REPO/releases/download/v0.1.0/final.mp4)

---

## Use it as a framework

```go
import "github.com/zigamedved/agent-exchange/pkg/platform"

// Public marketplace with invite-gated registration
p := platform.New(
    platform.WithStore(sqliteStore),
    platform.WithRegistration(platform.RegistrationInvite),
    platform.WithInviteStore(invites),
    platform.WithDefaultCredits(50),
    platform.WithOnCall(func(rec *platform.CallRecord) {
        // send to your billing system
    }),
)

// Internal enterprise exchange
p := platform.New(
    platform.WithStore(sqliteStore),
    platform.WithRegistration(platform.RegistrationClosed),
    platform.WithOnCall(func(rec *platform.CallRecord) {
        // send to Datadog / Grafana
    }),
)

// Local development (zero config)
p := platform.New()

http.ListenAndServe(":8080", p.Handler())
```

---

## Why AgentExchange?

| Problem | Without AX | With AX |
|---|---|---|
| Cross-company agent calls | Custom per-integration HTTP | One API key, one endpoint |
| Capability discovery | Email/Slack + docs | `GET /platform/v1/agents?skill=summarize` |
| Auth between orgs | Manual key exchange | Platform-managed, per-org keys |
| Billing | Invoice at month end | Per-call metering, credits, hooks |
| Observability | Nothing | Live dashboard, latency, spend |
| Agent visibility | Everything public | Public / private per agent |

---

## A2A Native

AgentExchange implements the A2A v1.0.0 protocol and extends it:

**A2A standard:** Agent Cards, `a2a_sendMessage`, `a2a_sendStreamingMessage`, SSE streaming, standard error codes.

**AX extensions** (gracefully ignored by A2A clients):
- `x-ax-pricing` — pricing in Agent Cards
- `x-ax-pubkey` + `x-ax-sig` — Ed25519 message signing
- `ax_quoteRequest` / `ax_quoteAccept` — price negotiation
- Platform routing, auth, metering, and observability

---

## Quick Start

### Run locally (Go toolchain)

```bash
# Terminal 1: start the exchange
make serve

# Terminal 2: start the code analyzer agent
make analyzer

# Terminal 3: discover and call
go run ./cmd/ax discover --api-key ax_companya_demo --skill code-analysis
go run ./cmd/ax call --to http://localhost:8080/platform/v1/route/<agent-id> \
  --api-key ax_companya_demo --text 'func hello() { fmt.Println("hi") }'

# Open the dashboard
open http://localhost:8080
```

### Run with Docker Compose

Spin up the platform plus the writer and analyst demo agents in one command:

```bash
make dev    # builds images and starts all services in the background
make down   # stop and remove all containers when you're done
```

Services exposed:

| Service  | Port  |
|----------|-------|
| Platform | 8080  |
| Writer   | 8082  |
| Analyst  | 8083  |

**Once the stack is up:**

```bash
# Open the live dashboard (agents, credit spend, call history)
open http://localhost:8080

# Discover registered agents
go run ./cmd/ax discover --api-key ax_companya_demo

# Filter by skill tag (exact match against skill ID or tag)
go run ./cmd/ax discover --api-key ax_companya_demo --skill writing

# Call an agent (replace <agent-id> with an ID from discover)
go run ./cmd/ax call \
  --to http://localhost:8080/platform/v1/route/<agent-id> \
  --api-key ax_companya_demo \
  --text "Write a short post about Go generics"

# Streaming call
go run ./cmd/ax call \
  --to http://localhost:8080/platform/v1/route/<agent-id> \
  --api-key ax_companya_demo \
  --text "Summarise this for me" --stream

# Run the orchestrator demo (discovers agents and routes calls between them)
make researcher

# Tail logs
docker compose logs -f            # all services
docker compose logs -f platform   # platform only
```

### Claude Code integration (MCP)

AX ships with an MCP server. Add to your project's `.mcp.json` (the MCP client uses the workspace root as the working directory):

```json
{
  "mcpServers": {
    "agent-exchange": {
      "command": "go",
      "args": ["run", "./cmd/mcp"],
      "env": { "AX_PLATFORM_URL": "http://localhost:8080" }
    }
  }
}
```

Claude Code gets `ax_discover`, `ax_call`, `ax_list_agents`, and `ax_my_org` tools. On first use, the MCP server auto-registers a private org with free credits.

---

## Architecture

```
Agent A ──→ AX Platform ──→ Agent B
            │
            ├── Auth (API keys, org-scoped)
            ├── Registry (discover by skill/org/name)
            ├── Routing (proxy calls between orgs)
            ├── Metering (per-call, per-org spend)
            ├── Credits (deduct on cross-org calls)
            ├── Signatures (Ed25519 verification)
            ├── Quotes (price negotiation)
            └── Dashboard (real-time SSE)
```

---

## Package Structure

```
pkg/                              ← the framework
  platform/                       ← exchange server (configurable via options)
    auth.go                       ← Auth interface + MemoryAuth
    invite.go                     ← InviteStore interface + MemoryInviteStore
    meter.go                      ← call metering + quote tracking
    platform.go                   ← routing, endpoints, options pattern
    dashboard.go                  ← embedded web dashboard
  protocol/                       ← A2A + AX types (pure data, no deps)
  registry/                       ← Store interface + MemoryStore + SQLiteStore
  transport/http/                 ← agent HTTP server + client + SSE
  identity/                       ← Ed25519 signing and verification

cmd/                              ← reference binaries
  ax/                             ← CLI (serve, discover, call, keys)
  mcp/                            ← MCP server for Claude Code
  platform/                       ← standalone platform server

examples/
  marketplace/                    ← public marketplace (invite-gated, credits, SQLite)
  enterprise/                     ← internal exchange (closed, no billing, SQLite)
  code-analyzer/                  ← sample agent (static code analysis)
  company-a-researcher/           ← demo orchestrator agent
  company-b-writer/               ← demo streaming agent
  company-c-analyst/              ← demo analysis agent
```

---

## Configuration

### Platform Options

| Option | Description | Default |
|---|---|---|
| `WithStore(s)` | Registry backend (`MemoryStore`, `SQLiteStore`, or custom) | `MemoryStore` |
| `WithAuth(a)` | Auth backend (implement `Auth` interface) | `MemoryAuth` |
| `WithRegistration(mode)` | `Open`, `Invite`, or `Closed` | `Open` |
| `WithDefaultCredits(n)` | Credits given to new orgs | `100` |
| `WithOnCall(hook)` | Callback after each routed call | `nil` |
| `WithInviteStore(s)` | Invite store for invite-gated mode | `nil` |
| `WithSignatureVerification(b)` | Enforce Ed25519 signatures | `false` |
| `WithLogger(l)` | Custom slog logger | `slog.Default()` |

### Interfaces

**Auth** — plug in your own org/key management:

```go
type Auth interface {
    Authenticate(apiKey string) *Org
    Register(name string, visibility OrgVisibility) (*Org, error)
    GetByID(id string) *Org
    All() []*Org
    DeductCredits(apiKey string, amount float64) error
    AddCredits(apiKey string, amount float64) error
}
```

**Store** — plug in your own agent registry:

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

**InviteStore** — plug in your own invite system:

```go
type InviteStore interface {
    Create(createdBy string) (string, error)
    Validate(code string) error
    Redeem(code string, orgID string) error
    List() []*Invite
}
```

---

## Org & Agent Visibility

- **Public org** — listed as a publisher on the marketplace
- **Private org** — consumer only, not listed
- **Public agent** — discoverable by all orgs
- **Private agent** — only visible within the owning org

Intra-org calls are always free (no credit deduction).

---

## CLI

```bash
ax serve                                    # start the exchange
ax discover --skill code-analysis           # find agents
ax call --to <url> --text "analyze this"    # call an agent
ax call --to <url> --text "..." --stream    # streaming call
ax card --url http://localhost:8082         # fetch agent card
ax keys generate                            # generate Ed25519 keys
```

---

## Building an Agent

```go
type MyAgent struct {
    card *protocol.AgentCard
}

func (a *MyAgent) Card() *protocol.AgentCard { return a.card }

func (a *MyAgent) HandleMessage(ctx context.Context, params *protocol.SendMessageParams) (*protocol.Task, *protocol.Message, error) {
    // Your agent logic here
    return task, nil, nil
}

// Start serving
server := axhttp.NewServer(agent)
http.ListenAndServe(":9000", server)
```

See `examples/code-analyzer/` for a complete working agent.

---

## Protocol Specification

See [SPEC.md](SPEC.md) for the full protocol specification including message format, signing, quotes, and error codes.

## License

Apache 2.0
