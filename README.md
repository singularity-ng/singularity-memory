# Singularity Memory

A standalone memory server for AI agents — Postgres-backed, MCP+HTTP
native, with BM25 + vector + RRF fusion retrieval and optional reranking.
The same running server can be shared across **Hermes**, **OpenClaw**,
**Claude Code** (and any other MCP-aware client) so memories persist and
move with the user, not the tool.

```
src/
  singularity_memory/             # CLI shim (singularity-memory serve|mcp|status)
  singularity_memory_server/      # previous engine/reference — HTTP + MCP server, retrieval pipeline
  singularity_memory_client/      # Python HTTP client (used by extensions)
  singularity_memory_client_api/  # OpenAPI-generated Python client
go/
  cmd/singularity-memory-go/       # target Go server runtime
extensions/
  hermes/                          # Python plugin — Hermes adapter (HTTP client)
  openclaw/                        # TypeScript plugin — OpenClaw adapter (HTTP client)
  mcp/                             # Wire-up recipes for Claude Code, Cursor, Windsurf, OpenCode
```

The target server runtime lives in `go/`. The directories under `extensions/`
are thin adapters — none of them duplicate retrieval logic; they all forward
to the running server.

## Quick start

```bash
# Bring up the server (Postgres 18 + VectorChord suite + Singularity Memory):
docker compose up singularity-memory-postgres singularity-memory

# Or, run the Go server against your own Postgres with vchord installed:
cd go
SINGULARITY_DATABASE_URL=postgresql://user:pw@host:5432/db \
SINGULARITY_STORAGE_PROFILE=vchord \
SINGULARITY_FEATURE_BANKS=true \
SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1 \
SINGULARITY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1 \
  go run ./cmd/singularity-memory-go --host 0.0.0.0
```

Verify the current Go slice is up:

```bash
curl -s http://localhost:8888/healthz | jq .
curl -s http://localhost:8888/v1/banks | jq .
```

Manage the shared model catalog:

```bash
cd go
curl -s -X POST http://localhost:8888/v1/model-catalog/sync | jq '.sources'
curl -s http://localhost:8888/v1/model-catalog/export/sf | jq '.policy'
go run ./cmd/modelwalk --server http://localhost:8888
go run ./cmd/modelwalk --server http://localhost:8888 --ssh :23235
```

The model catalog is daemon-owned state. `modelwalk` is the Charmbracelet
management UI, and SF should consume `/v1/model-catalog/export/sf` rather than
owning provider normalization itself. Charm Soft Serve can host a Git-backed
overlay repo later, but the live data plane stays in Singularity Memory.

## Wire it up

| Client       | How                                                                          |
|--------------|------------------------------------------------------------------------------|
| **Hermes**   | Symlink `extensions/hermes/` into `$HERMES_HOME/plugins/singularity_memory/`. Edit `$HERMES_HOME/singularity-memory.json` to set `server_url`. |
| **OpenClaw** | `npm install @singularity-memory/openclaw-plugin`, set `serverUrl` in plugin config. See `extensions/openclaw/README.md`. |
| **Claude Code** | `claude mcp add --transport http singularity http://localhost:8888/mcp/`. See `extensions/mcp/`. |
| **Cursor / Windsurf / OpenCode** | Drop the appropriate `.mcp.json` snippet from `extensions/mcp/`. |

## What lives where

### `go/` — target runtime

Go server runtime for the migration. It owns the new HTTP/MCP/worker/retrieval
path and targets Postgres 18 + VectorChord.

Current slice: health/version plus bank endpoints behind
`SINGULARITY_FEATURE_BANKS=true`.

### `src/singularity_memory_server/` — previous engine/reference

Originally derived from [vectorize-io/hindsight](https://github.com/vectorize-io/hindsight)
(MIT) and assimilated into this codebase under our namespace; see `NOTICE`.
Provides retain / recall / reflect operations, Banks, entities, mental
models, audit logs, async workers, alembic migrations, and the FastAPI
HTTP + MCP server.

A ports backlog from the retired in-tree engine lives at
[`src/singularity_memory_server/BACKLOG.md`](./src/singularity_memory_server/BACKLOG.md):
six features (`embeddings_pending` metric, `vector_enabled` opt-in,
auto-backfill, lane weighting, two-tier reranking, helpfulness feedback)
that landed in the hand-written engine and want a real Postgres+vchord
test environment to land in B safely.

The staged Python-to-Go migration plan lives in [`MIGRATION.md`](./MIGRATION.md).
The current rule is contract-first: preserve the committed HTTP/MCP wire
contract while moving the server runtime to Go.

### `extensions/hermes/` — Python plugin

Thin Hermes `MemoryProvider` that forwards every call to the running
server over HTTP. ~330 lines, no in-process retrieval. Implements
`initialize` / `prefetch` / `sync_turn` / `handle_tool_call` / setup
wizard config schema.

Set `server_url` in `$HERMES_HOME/singularity-memory.json`. Multiple Hermes
sessions share one Singularity Memory server backed by the Postgres 18
VectorChord container.

### `extensions/openclaw/` — TypeScript plugin

OpenClaw plugin (`@singularity-memory/openclaw-plugin`) that hooks
`before_prompt_build` (auto-recall) and `agent_end` (auto-capture) to
forward to the same HTTP server. Modeled after OpenClaw's in-tree
`memory-lancedb` plugin, swapping LanceDB for our server.

### `extensions/mcp/` — recipes

`.mcp.json` snippets for Claude Code, Cursor, Windsurf, OpenCode. No
plugin to install on the client side — the server's `/mcp/` endpoint
speaks JSON-RPC over HTTP and any MCP-aware client connects in one
config line.

## Retrieval stack

- **Production vector**: `vchord` / `vchordrq` on external Postgres.
  Configure via `SINGULARITY_VECTOR_EXTENSION=vchord`.
- **Production lexical**: `vchord_bm25` on external Postgres. Configure via
  `SINGULARITY_TEXT_SEARCH_EXTENSION=vchord`.
- **Fusion**: Reciprocal Rank Fusion across lanes.
- **Reranking**: optional cross-encoder via OpenAI-compatible HTTP
  endpoint (e.g. Qwen3 reranker on a local LLM gateway).
- **Graph**: relational link expansion in the current server. Apache AGE is
  not required for the first Go migration.

The migration target is vchord-first because it supports the scale,
high-dimensional embeddings, and real BM25 path we want. `pg0` embedded mode,
Apache AGE, and TimescaleDB are outside the first Go migration unless a
concrete runtime query path needs them.

## Configuration

All server config flows through `SINGULARITY_*` environment variables. For the
Go runtime, `go/internal/config/config.go` is the source of truth; the Python
config is reference history during the migration. Common ones:

| Var                                       | Purpose                              |
|-------------------------------------------|--------------------------------------|
| `SINGULARITY_DATABASE_URL`                | Postgres DSN                         |
| `SINGULARITY_HOST` / `SINGULARITY_PORT`   | Bind host / port (default 127.0.0.1:8888) |
| `SINGULARITY_MCP_ENABLED`                 | Enable `/mcp/` (default true)        |
| `SINGULARITY_LLM_PROVIDER`                | `openai` / `anthropic` / `none` etc. |
| `SINGULARITY_LLM_API_KEY`                 | Forwarded to the LLM provider        |
| `SINGULARITY_EMBEDDINGS_PROVIDER`         | `local` / `openai` / `cohere` etc.   |
| `SINGULARITY_EMBEDDINGS_OPENAI_API_KEY`   | Embedding endpoint key               |
| `SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL`  | `https://llm-gateway.centralcloud.com/v1` |
| `SINGULARITY_EMBEDDINGS_OPENAI_MODEL`     | Embedding model, e.g. `qwen/qwen3-embedding-4b` |
| `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` | Optional output dimensions; unset uses model-native size |
| `SINGULARITY_RERANK_OPENAI_BASE_URL`      | `https://llm-gateway.centralcloud.com/v1` |
| `SINGULARITY_RERANK_MODEL`                | Rerank model, e.g. `qwen/qwen3-reranker-4b` |
| `SINGULARITY_MODEL_CATALOG_PATH`          | JSON cache for Catwalk/models.dev normalized catalog |
| `SINGULARITY_VECTOR_EXTENSION`            | `vchord`                             |
| `SINGULARITY_TEXT_SEARCH_EXTENSION`       | `vchord`                             |

## License & attribution

MIT. See `LICENSE`. Engine code derived from
[`vectorize-io/hindsight`](https://github.com/vectorize-io/hindsight) (also
MIT) and assimilated here; see `NOTICE` for details.
