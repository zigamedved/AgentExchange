# FAXP Demo — Three Companies, One Platform

This demo shows three AI agents from three "different companies" communicating through the FAXP platform — without any direct knowledge of each other's URLs at startup.

---

## The Scenario

| Company | Agent | Capability | Price |
|---|---|---|---|
| Company A | Researcher | Orchestrates the workflow | — |
| Company B | Writer | Generates markdown reports | $0.005/call |
| Company C | Analyst | Extracts structured metrics from text | $0.002/call |

**What happens:**
1. Company A's researcher queries the platform: *"Who can generate reports?"*
2. Platform returns Company B's writer (discovered via the registry)
3. Researcher routes a **streaming** call through the platform → writer streams back a report in chunks
4. Researcher routes the completed report to Company C's analyst
5. Analyst returns structured JSON: word count, key topics, sentiment, reading time
6. All calls are metered and appear on the live dashboard in real time

---

## Running the Demo

You need four terminal windows (or use `make demo`).

### Terminal 1 — Platform

```bash
make platform
# or: go run ./cmd/platform
```

Open the dashboard: **http://localhost:8080**

You'll see it shows "No agents registered" — watch it populate in the next steps.

### Terminal 2 — Company B (Writer Agent)

```bash
make writer
# or:
FAXP_PLATFORM_URL=http://localhost:8080 \
FAXP_API_KEY=faxp_companyb_demo \
FAXP_AGENT_URL=http://localhost:8082 \
FAXP_AGENT_PORT=8082 \
go run ./examples/company-b-writer
```

Watch the dashboard: the writer agent appears immediately.

### Terminal 3 — Company C (Analyst Agent)

```bash
make analyst
```

Dashboard now shows both agents with their skills and pricing.

### Terminal 4 — Company A (Researcher, runs and exits)

```bash
make researcher
```

Expected output:

```
═══════════════════════════════════════════════════════
  FAXP Demo — Company A Researcher
═══════════════════════════════════════════════════════
  Topic: AI Agent Protocols and Distributed Systems in 2026

── Step 1: Discovering report_generation agents via platform registry...
  ✓ Found: company-b-writer (id: fa69babd)

── Step 2: Discovering text_analysis agents via platform registry...
  ✓ Found: company-c-analyst (id: fc53acef)

── Step 3: Routing streaming call → company-b-writer (via platform)...

# Research Report: ...
[report streams in real time]

── Step 4: Routing call → company-c-analyst (via platform)...

── Step 5: Final Analysis Results

{
  "word_count": 170,
  "sentence_count": 10,
  "key_topics": ["agent", "integration", "strategic", ...],
  "sentiment": "neutral",
  "sentiment_score": 0.14,
  "reading_time_min": 0.9,
  ...
}

  Demo complete. Check the dashboard: http://localhost:8080
```

---

## What the Dashboard Shows

- **Registered Agents** panel: Company B and Company C, with their skills and per-call pricing
- **Live Call Feed**: each call as it flows through the platform — caller org, target agent, method, latency, status
- **Spend by Organization**: Company A accumulating $0.005 + $0.002 = $0.007 in this run

---

## Testing Individual Pieces with fixctl

```bash
# Fetch Company B's Agent Card directly
go run ./cmd/fixctl card --url http://localhost:8082

# List all registered agents via the platform
go run ./cmd/fixctl registry ls --api-key faxp_companya_demo

# Find agents by skill
go run ./cmd/fixctl registry find --skill report_generation --api-key faxp_companya_demo

# Send a direct message to Company B (bypasses platform)
go run ./cmd/fixctl send --to http://localhost:8082 --text "Write a report about Go programming"

# Stream a message directly
go run ./cmd/fixctl send --to http://localhost:8082 --text "Write a report about Go programming" --stream

# Generate a new Ed25519 key pair
go run ./cmd/fixctl keys generate --out my-agent.key
go run ./cmd/fixctl keys show --key my-agent.key
```

---

## What This Demonstrates

| Capability | How it's shown |
|---|---|
| **Cross-company discovery** | Company A doesn't hardcode Company B's URL — it asks the registry |
| **Platform routing** | All calls proxy through the platform, not direct agent-to-agent |
| **SSE Streaming** | Writer streams the report in chunks, visible in terminal and dashboard |
| **Metering** | Every routed call is logged with latency and price |
| **Live observability** | Dashboard updates in real time via SSE event stream |
| **Agent Cards** | Each agent's capabilities are introspectable via `/.well-known/agent.json` |
| **A2A compatibility** | Any A2A-compatible client can call these agents directly |
