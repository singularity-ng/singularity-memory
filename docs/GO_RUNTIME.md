# Go Runtime

The active service is rooted at the repository root:

- `cmd/operations-memory-go`: HTTP/MCP server binary.
- `internal/httpapi`: HTTP routes and MCP tool bridge.
- `internal/store`: Postgres storage, schema helpers, core memory, brain pages,
  entity persistence, and retrieval support.
- `internal/retrieve`: semantic, VectorChord-BM25, graph, temporal, and RRF
  retrieval lanes.
- `docker/operations-memory`: container image for the Go service.

Current API surface:

- `GET /healthz`
- `GET /version`
- `GET /openapi.json`
- `GET /v1/banks`
- `GET /v1/default/banks`
- `GET /v1/default/banks/{bank_id}/profile`
- `PUT/PATCH/DELETE /v1/default/banks/{bank_id}`
- `POST /v1/default/banks/{bank_id}/memories`
- `POST /v1/default/banks/{bank_id}/memories/recall`
- `POST /v1/default/banks/{bank_id}/context`
- `GET/PUT/PATCH/DELETE /v1/default/banks/{bank_id}/core-memory/...`
- `POST /v1/default/banks/{bank_id}/consolidate`
- `GET /v1/default/banks/{bank_id}/reflect`
- Brain page/link/timeline/job endpoints under
  `/v1/default/banks/{bank_id}/brain/...`
- MCP JSON-RPC over `/mcp` and `/mcp/{bank_id}`

Configuration uses `OPS_MEMORY_*` environment variables only.

Run:

```bash
OPS_MEMORY_DATABASE_URL=postgresql://operations_memory:password@localhost:5432/operations_memory \
OPS_MEMORY_STORAGE_PROFILE=vchord \
OPS_MEMORY_FEATURE_BANKS=true \
OPS_MEMORY_FEATURE_MEMORIES=true \
  go run ./cmd/operations-memory-go
```

Test:

```bash
go test ./...
```
