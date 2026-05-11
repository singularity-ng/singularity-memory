# Operations Memory

Operations Memory is a Go memory service for AI agents. It is Postgres-backed
and uses VectorChord, VectorChord-BM25, RRF fusion, optional reranking, entity
links, core-memory blocks, and agent-brain pages.

## Layout

```text
cmd/operations-memory-go/   HTTP/MCP server
internal/                   service packages
docker/operations-memory/   service image
docker/postgres-vchord/     Postgres 18 + VectorChord image
docs/                       current operating docs
```

The active tree is Go-only. Runtime configuration is `OPS_MEMORY_*` only.

## Run

```bash
docker compose up operations-memory-postgres operations-memory
```

Or run against an existing Postgres 18 + VectorChord database:

```bash
OPS_MEMORY_DATABASE_URL=postgresql://user:pw@host:5432/db \
OPS_MEMORY_STORAGE_PROFILE=vchord \
OPS_MEMORY_FEATURE_BANKS=true \
OPS_MEMORY_FEATURE_MEMORIES=true \
OPS_MEMORY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1 \
OPS_MEMORY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1 \
  go run ./cmd/operations-memory-go --host 0.0.0.0
```

## Verify

```bash
curl -s http://localhost:8888/healthz | jq .
curl -s http://localhost:8888/v1/banks | jq .
curl -s -X POST http://localhost:8888/v1/default/banks/default/context \
  -H 'content-type: application/json' \
  -d '{"query":"what matters now?","max_tokens":1200}' | jq .
```

## Test

```bash
go test ./...
```
