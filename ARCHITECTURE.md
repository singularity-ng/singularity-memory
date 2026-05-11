# Architecture

Operations Memory is a single Go service backed by Postgres 18 with
VectorChord and VectorChord-BM25.

## Runtime

- `cmd/operations-memory-go` starts the HTTP/MCP server.
- `internal/httpapi` owns routes, request/response shapes, and MCP tool
  delegation.
- `internal/store` owns Postgres access, schema helpers, core memory blocks,
  brain pages, entity persistence, and consolidation/reflect helpers.
- `internal/retrieve` owns semantic, BM25, graph, temporal, and RRF retrieval.
- `internal/embed` and `internal/rerank` call OpenAI-compatible gateways.

## Storage

The storage target is external Postgres 18 with:

- `vector`
- `vchord`
- `pg_tokenizer`
- `vchord_bm25`
- `pg_trgm`

The active deployment path is Docker Compose or a manually managed Postgres
with the same extensions installed.
