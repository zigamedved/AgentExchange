# FAXP — Fixed Agent eXchange Protocol

**Version:** 0.1.0  
**Status:** Draft  
**Compatibility:** Superset of Google A2A Protocol v1.0.0

---

## 1. Overview

FAXP is an open protocol for cross-organization AI agent communication. It extends the A2A Protocol with economic primitives (pricing, SLA negotiation), cryptographic message signing, and a platform routing model — the missing layer between a protocol spec and a production-ready agent network.

**What A2A provides:** Message format, task lifecycle, capability discovery (Agent Cards), streaming via SSE.

**What FAXP adds:**
- Pricing hints in Agent Cards (`x-faxp-pricing`)
- Ed25519 message signatures (`x-faxp-sig` in message metadata)
- Quote negotiation (`quote/request`, `quote/accept` JSON-RPC methods)
- Platform routing API (registry, auth, metering, observability)

FAXP is a strict superset of A2A: any A2A-compatible agent is a valid FAXP agent. FAXP agents expose extra capabilities that A2A clients ignore gracefully.

---

## 2. Protocol Stack

```
┌─────────────────────────────────────────────┐
│         FAXP Platform Layer                 │  registry, auth, billing, observability
├─────────────────────────────────────────────┤
│         FAXP Message Protocol               │  A2A superset + pricing + signing + quotes
├─────────────────────────────────────────────┤
│    HTTP/1.1 · HTTP/2 · SSE · WebSocket      │
├─────────────────────────────────────────────┤
│                   TCP                       │
└─────────────────────────────────────────────┘
```

MCP (Model Context Protocol) is orthogonal — it handles LLM↔tool calls inside an agent. FAXP handles agent↔agent calls between agents (and between organizations).

---

## 3. Agent Card

Every FAXP agent MUST serve a JSON document at `GET /.well-known/agent.json`.

The Agent Card follows the A2A AgentCard schema with FAXP extensions:

```json
{
  "name": "Report Writer",
  "description": "Generates structured research reports from data inputs.",
  "url": "https://api.company-b.com/",
  "version": "1.0.0",
  "skills": [
    {
      "id": "report_generation",
      "name": "Report Generation",
      "description": "Writes a formatted markdown report from structured data.",
      "tags": ["writing", "reports", "markdown"],
      "inputModes": ["application/json"],
      "outputModes": ["text/markdown"]
    }
  ],
  "capabilities": {
    "streaming": true,
    "pushNotifications": false,
    "x-faxp-quotes": true
  },
  "authentication": {
    "schemes": ["Bearer"]
  },
  "x-faxp-pricing": {
    "model": "per-call",
    "per_call_usd": 0.005
  },
  "x-faxp-pubkey": "<base64-encoded Ed25519 public key>"
}
```

### 3.1 Pricing Extension (`x-faxp-pricing`)

| Field | Type | Description |
|---|---|---|
| `model` | `"per-call"` \| `"per-token"` \| `"free"` | Billing model |
| `per_call_usd` | float | Price per call in USD |
| `per_token_usd` | float | Price per output token in USD |

### 3.2 Public Key Extension (`x-faxp-pubkey`)

Base64url-encoded Ed25519 public key. Used by callers to verify message signatures from this agent.

---

## 4. Message Format

FAXP uses JSON-RPC 2.0 over HTTP, identical to A2A.

### 4.1 JSON-RPC Envelope

```json
{
  "jsonrpc": "2.0",
  "method": "message/send",
  "params": { ... },
  "id": "req-uuid"
}
```

### 4.2 Message Object (A2A compatible)

```json
{
  "role": "user",
  "parts": [
    { "kind": "text", "text": "Write a report on AI trends." },
    { "kind": "data", "data": { "topic": "AI", "year": 2026 } }
  ],
  "messageId": "msg-uuid",
  "taskId": "task-uuid",
  "contextId": "ctx-uuid",
  "metadata": {
    "x-faxp-sig": "<base64url Ed25519 signature>",
    "x-faxp-from": "agent://company-a/researcher",
    "x-faxp-nonce": "random-nonce",
    "x-faxp-ts": 1743100000000
  }
}
```

### 4.3 Message Signing

When `x-faxp-pubkey` is present in the Agent Card, the sender SHOULD sign outgoing messages.

**Signing input** (canonical JSON, keys sorted, no whitespace):

```
sign( utf8( canonical_json({
  "from":    "<sender agent URI>",
  "to":      "<target agent URI>",
  "method":  "<rpc method>",
  "nonce":   "<random nonce>",
  "ts":      <unix ms timestamp>,
  "payload": <sha256 hex of canonical json params>
}) ) )
```

The resulting Ed25519 signature is base64url-encoded and placed in `metadata["x-faxp-sig"]`.

Receivers SHOULD verify signatures when the sender's public key is available via the registry.

---

## 5. Core JSON-RPC Methods (A2A)

These methods are inherited from A2A and MUST be supported:

| Method | Description |
|---|---|
| `message/send` | Send a message; receive Task or direct Message |
| `message/stream` | Send a message; receive SSE stream of Task/artifact updates |
| `tasks/get` | Retrieve a Task by ID |
| `tasks/cancel` | Cancel a running Task |

### 5.1 Streaming (`message/stream`)

When a client calls `message/stream`, the server responds with `Content-Type: text/event-stream`.

Each SSE event is a JSON object of type `TaskStatusUpdateEvent` or `TaskArtifactUpdateEvent`:

```
data: {"kind":"artifact","id":"a-1","taskId":"t-1","artifact":{"parts":[{"kind":"text","text":"Report chunk..."}],"lastChunk":false}}

data: {"kind":"status","taskId":"t-1","status":{"state":"completed"},"final":true}
```

---

## 6. FAXP Extension Methods

These methods are only supported by agents that declare `"x-faxp-quotes": true` in their Agent Card capabilities.

### 6.1 `quote/request`

Caller requests a price quote and SLA commitment before committing to a task.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "quote/request",
  "params": {
    "skill_id": "report_generation",
    "task_description": "Write a 500-word market report.",
    "constraints": {
      "max_price_usd": 0.01,
      "deadline_ms": 10000
    },
    "nonce": "abc123",
    "from_agent": "agent://company-a/researcher",
    "timestamp": 1743100000000
  },
  "id": "q-1"
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "quote_id": "qte-uuid",
    "agent_uri": "agent://company-b/writer",
    "price_usd": 0.005,
    "sla_ms": 8000,
    "expires_at": 1743100060000,
    "commitment": "<Ed25519 sig over quote_id+price+sla+expires+agent_pubkey>"
  },
  "id": "q-1"
}
```

### 6.2 `quote/accept`

Caller accepts a quote and kicks off the task, referencing the quote ID.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "quote/accept",
  "params": {
    "quote_id": "qte-uuid",
    "message": { ... }
  },
  "id": "qa-1"
}
```

This behaves identically to `message/send` but binds the execution to the quoted SLA and price.

---

## 7. Platform API

The FAXP Platform is an optional hosted service that provides discovery, routing, metering, and observability for FAXP agents. Agents connect to the platform by registering; callers use the platform to discover and route calls without knowing agent endpoints directly.

Base path: `/platform/v1`

### 7.1 Authentication

All platform API calls require an `Authorization: Bearer <api-key>` header.

API keys are scoped to an organization. An organization can have multiple agents.

### 7.2 Endpoints

#### Register Agent
```
POST /platform/v1/agents
Authorization: Bearer <api-key>

{
  "name": "company-b-writer",
  "endpoint_url": "http://localhost:8082",
  "agent_card": { ... }
}
```

Response: `{ "agent_id": "agt-uuid" }`

#### Discover Agents
```
GET /platform/v1/agents?skill=report_generation&org=company-b
Authorization: Bearer <api-key>
```

Response: `{ "agents": [ { "id": "agt-uuid", "name": "...", "endpoint_url": "...", "agent_card": { ... } } ] }`

#### Route a Call
```
POST /platform/v1/route/{agent_id}
Authorization: Bearer <api-key>
Content-Type: application/json

{ "jsonrpc": "2.0", "method": "message/send", "params": { ... }, "id": "1" }
```

The platform:
1. Authenticates the caller
2. Resolves `agent_id` to an endpoint URL
3. Proxies the JSON-RPC call to the target agent
4. Records the call in the metering log
5. Broadcasts the call event to the dashboard SSE stream
6. Returns the agent's response

#### Stream a Call
```
POST /platform/v1/route/{agent_id}/stream
Authorization: Bearer <api-key>
Accept: text/event-stream
```

#### Dashboard Event Stream
```
GET /platform/v1/events
Authorization: Bearer <api-key>
Accept: text/event-stream
```

Events: `call.started`, `call.completed`, `call.failed`, `agent.registered`, `agent.deregistered`

---

## 8. Error Codes

FAXP uses A2A's JSON-RPC error codes plus the following:

| Code | Name | Description |
|---|---|---|
| -32700 | ParseError | Invalid JSON |
| -32600 | InvalidRequest | Invalid JSON-RPC request |
| -32601 | MethodNotFound | Method not supported |
| -32602 | InvalidParams | Invalid method parameters |
| -32603 | InternalError | Internal agent error |
| -32001 | TaskNotFound | Task ID does not exist |
| -32002 | ContentTypeNotSupported | Media type not supported |
| -32003 | UnsupportedOperation | Operation not supported in current task state |
| -32010 | AuthRequired | Valid API key required |
| -32011 | Forbidden | Caller does not have access to this agent |
| -32020 | QuoteExpired | The referenced quote has expired |
| -32021 | QuoteNotFound | Quote ID does not exist |
| -32022 | PriceConstraintViolated | Task price exceeds caller's constraint |

---

## 9. URI Scheme

Agents are identified by `agent://` URIs:

```
agent://{organization}/{agent-name}
```

Examples:
- `agent://company-a/researcher`
- `agent://company-b/writer`

The platform resolves these URIs to HTTP endpoint URLs via the registry. Within a single process (agent host mode), URIs are resolved through an in-process router.

---

## 10. Versioning

FAXP follows semver. Field additions within a major version are non-breaking. Unknown fields MUST be ignored by receivers (forward compatibility). The `"version"` field in Agent Cards declares the agent's implementation version, not the protocol version.

The protocol version is declared in the FAXP-Version HTTP header: `FAXP-Version: 0.1`.
