# Ops Memory Provider

Postgres-backed memory with BM25 + vector + RRF fusion retrieval and optional
cross-encoder reranking. Per-agent private banks plus a shared bank for
cross-agent context (runbooks, incident outcomes, failure fingerprints).

## Requirements

- Operations Memory server running (`git.infra.centralcloud.com/centralcloud/operations-memory`)
- Postgres + VectorChord (`vchord`) on the server side

## Setup (in Hermes config.yaml)

```yaml
memory:
  provider: ops_memory
```

Env vars (set in pod deployment):

| Env var | Description |
|---------|-------------|
| `OPS_MEMORY_URL` | Server base URL (e.g. `http://operations-memory.centralcloud-ops-memory.svc:8888`) |
| `OPS_MEMORY_BANK` | Bank ID for this agent (e.g. `ops-agent`, `incident-commander`) |
| `OPS_MEMORY_KEY` | API key if server requires auth (optional) |

## Tools

| Tool | Description |
|------|-------------|
| `sm_recall` | BM25 + vector search with optional reranking |
| `sm_remember` | Explicitly persist a fact to long-term memory |
| `sm_forget` | Delete a specific memory by ID |

## Bank layout

| Bank | Purpose |
|------|---------|
| `shared` | Runbooks, incident outcomes, failure fingerprints (all agents R/W) |
| `ops-agent` | Staff conversations, HostBill context (private) |
| `incident-commander` | Incident working memory (private) |
| `comms-agent` | Comms drafts, notification history (private) |
| `router` | Routing patterns (private) |
