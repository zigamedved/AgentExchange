# AgentExchange (AX) — Protocol Extensions

**Version:** 0.1.0
**Status:** Draft
**Compatibility:** A2A Protocol v1.0.0 native

---

## 1. Overview

AgentExchange is an A2A-native platform for cross-organization AI agent communication. It extends the A2A Protocol with economic primitives (pricing, SLA negotiation), cryptographic message signing, and a platform routing model.

**What A2A provides:** Message format, task lifecycle, capability discovery (Agent Cards), streaming via SSE.

**What AX adds:**
- Pricing hints in Agent Cards (`x-ax-pricing`)
- Ed25519 message signatures (`x-ax-sig` in message metadata)
- Quote negotiation (`ax_quoteRequest`, `ax_quoteAccept` JSON-RPC methods)
- Platform routing API (registry, auth, metering, observability)

AX is fully A2A-native: any A2A-compatible agent works with the exchange. AX extensions are additive and gracefully ignored by standard A2A clients.

---

## 2. Protocol Stack

```
┌─────────────────────────────────────────────┐
│         AX Platform Layer                   │  registry, auth, billing, observability
├─────────────────────────────────────────────┤
│         A2A Message Protocol + AX ext.      │  pricing + signing + quotes
├─────────────────────────────────────────────┤
│    HTTP/1.1 · HTTP/2 · SSE · WebSocket      │
├─────────────────────────────────────────────┤
│                   TCP                       │
└─────────────────────────────────────────────┘
```

MCP (Model Context Protocol) is orthogonal. It handles LLM-to-tool calls inside an agent. A2A handles agent-to-agent calls between agents (and between organizations). AX adds the platform layer on top.

---

## 3. Agent Card

Every A2A agent MUST serve a JSON document at `GET /.well-known/a2a/agent-card.json`.

The Agent Card follows the A2A AgentCard schema. AX adds optional extensions:

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
    "x-ax-quotes": true
  },
  "authentication": {
    "schemes": ["Bearer"]
  },
  "x-ax-pricing": {
    "model": "per-call",
    "per_call_usd": 0.005
  },
  "x-ax-pubkey": "<base64-encoded Ed25519 public key>"
}
```

### 3.1 Pricing Extension (`x-ax-pricing`)

| Field | Type | Description |
|---|---|---|
| `model` | `"per-call"` \| `"per-token"` \| `"free"` | Billing model |
| `per_call_usd` | float | Price per call in USD |
| `per_token_usd` | float | Price per output token in USD |

### 3.2 Public Key Extension (`x-ax-pubkey`)

Base64url-encoded Ed25519 public key. Used by callers to verify message signatures from this agent.

---

## 4. Message Format

AX uses JSON-RPC 2.0 over HTTP, identical to A2A.

### 4.1 JSON-RPC Envelope

```json
{
  "jsonrpc": "2.0",
  "method": "a2a_sendMessage",
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
    "x-ax-sig": "<base64url Ed25519 signature>",
    "x-ax-from": "agent://company-a/researcher",
    "x-ax-nonce": "random-nonce",
    "x-ax-ts": 1743100000000
  }
}
```

### 4.3 Message Signing

When `x-ax-pubkey` is present in the Agent Card, the sender SHOULD sign outgoing messages.

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

The resulting Ed25519 signature is base64url-encoded and placed in `metadata["x-ax-sig"]`.

Receivers SHOULD verify signatures when the sender's public key is available via the registry.

---

## 5. Core JSON-RPC Methods (A2A)

These methods are inherited from A2A and MUST be supported:

| Method | Description |
|---|---|
| `a2a_sendMessage` | Send a message; receive Task or direct Message |
| `a2a_sendStreamingMessage` | Send a message; receive SSE stream of Task/artifact updates |
| `a2a_getTask` | Retrieve a Task by ID |
| `a2a_cancelTask` | Cancel a running Task |

### 5.1 Streaming (`a2a_sendStreamingMessage`)

When a client calls `a2a_sendStreamingMessage`, the server responds with `Content-Type: text/event-stream`.

Each SSE event is a JSON object of type `TaskStatusUpdateEvent` or `TaskArtifactUpdateEvent`:

```
data: {"kind":"artifact","id":"a-1","taskId":"t-1","artifact":{"parts":[{"kind":"text","text":"Report chunk..."}],"lastChunk":false}}

data: {"kind":"status","taskId":"t-1","status":{"state":"completed"},"final":true}
```

---

## 6. AX Extension Methods

These methods are only supported by agents that declare `"x-ax-quotes": true` in their Agent Card capabilities.

### 6.1 `ax_quoteRequest`

Caller requests a price quote and SLA commitment before committing to a task.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "ax_quoteRequest",
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

### 6.2 `ax_quoteAccept`

Caller accepts a quote and kicks off the task, referencing the quote ID.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "ax_quoteAccept",
  "params": {
    "quote_id": "qte-uuid",
    "message": { ... }
  },
  "id": "qa-1"
}
```

This behaves identically to `a2a_sendMessage` but binds the execution to the quoted SLA and price.

---

## 7. Platform API

The AX Platform is an optional hosted service that provides discovery, routing, metering, and observability for A2A agents. Agents connect to the platform by registering; callers use the platform to discover and route calls without knowing agent endpoints directly.

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

{ "jsonrpc": "2.0", "method": "a2a_sendMessage", "params": { ... }, "id": "1" }
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

AX uses A2A's JSON-RPC error codes plus the following:

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

AX follows semver. Field additions within a major version are non-breaking. Unknown fields MUST be ignored by receivers (forward compatibility). The `"version"` field in Agent Cards declares the agent's implementation version, not the protocol version.

The protocol version is declared in the AX-Version HTTP header: `AX-Version: 0.1`.
