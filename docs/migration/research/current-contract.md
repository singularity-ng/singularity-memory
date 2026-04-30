# Current HTTP/MCP Contract

This is the local evidence snapshot for freezing the Go migration contract.
Treat the Python implementation as contract history only; Go parity must be
proved from committed artifacts, tests, fixtures, and docs.

## HTTP Contract Artifact

- `openapi.json` is committed at the repo root and is the frozen HTTP contract
  for the migration. It declares OpenAPI `3.1.0`, title
  `Singularity Memory HTTP API`, version `0.5.3`.
- `scripts/dump-openapi.py` shows how the artifact was produced from the
  FastAPI app with migrations, MCP, LLM, embeddings, and reranking disabled.
- Go parity validates against the committed artifact. Regenerating from the old
  app is historical context only and must not be required in CI gates.
- The artifact includes `/health`, `/metrics`, `/version`, bank management,
  profile/config/stats, retain/recall, memories, documents, chunks, entities,
  graph, directives, mental models, operations, consolidation, files, audit
  logs, export/import, webhooks, and admin embedding endpoints.

## Implemented Go HTTP Surface

Local Go server evidence is in `go/internal/httpapi/server.go`,
`go/internal/httpapi/banks.go`, and `go/internal/httpapi/server_test.go`.

Implemented Go routes:

- `GET /healthz`
- `GET /version`
- `GET /openapi.json`
- `GET /v1/banks`
- `GET /v1/default/banks`
- `GET /v1/default/banks/{bank_id}/profile`
- `PUT /v1/default/banks/{bank_id}/profile`
- `PUT /v1/default/banks/{bank_id}`
- `PATCH /v1/default/banks/{bank_id}`
- `DELETE /v1/default/banks/{bank_id}`

The `/v1` bank routes are gated by the `banks` feature flag and return `404`
when disabled. Handler tests cover the compatibility envelope for listing
banks, the unscoped `/v1/banks` alias, profile fetch, profile auto-create
shape, update variants, delete shape, missing store `503`, version features,
OpenAPI serving from the committed artifact, and feature-flag behavior.

Contract gap: the Go route set is a small subset of `openapi.json`. Future Go
routes must be treated as unproven until they have fixture-backed parity tests.

## MCP Contract Surface

MCP-over-HTTP is documented in `extensions/mcp/README.md` as `/mcp/` on the
same port as HTTP, with examples for Claude Code, Cursor, Windsurf, OpenCode,
and generic MCP clients. The Python HTTP MCP middleware in
`src/singularity_memory_server/api/mcp.py` defines these contract behaviors:

- root `/mcp/` is multi-bank mode;
- `/mcp/{bank_id}/` is single-bank mode;
- bank resolution priority is URL path, then `X-Bank-Id`, then
  `SINGULARITY_MCP_BANK_ID` defaulting to `default`;
- auth accepts legacy `SINGULARITY_MCP_AUTH_TOKEN` or tenant extension auth;
- GET probes without `Mcp-Session-Id` return `200 {}`;
- the middleware adds `Accept: application/json, text/event-stream` when the
  client omits the MCP-required event-stream accept value.

The shared tool registry in `src/singularity_memory_server/mcp_tools.py`
explicitly lists the MCP tool names: `retain`, `sync_retain`, `recall`,
`reflect`, `list_banks`, `create_bank`, mental model tools, directive tools,
memory/document/operation browsing tools, `list_tags`, bank get/stats/update/
delete, and `clear_memories`. Tool schemas and transcripts still need to be
frozen as committed fixtures before Go MCP can claim parity.

## Fixture Normalization

No committed HTTP/MCP fixture directory exists yet. Add fixtures per endpoint
or tool before flipping traffic to Go.

Normalization rules for parity fixtures:

- compare parsed JSON, not raw encoder bytes;
- sort object keys before byte comparison;
- preserve array order unless the contract explicitly says the result is
  unordered;
- normalize timestamps to RFC3339 UTC strings;
- normalize UUID-like, operation-id, request-id, and duration fields only when
  the fixture marks them volatile;
- require exact status code, content type, header presence for documented
  headers, and response body shape;
- record any accepted normalization next to the fixture.

## Runtime Dependency Rule

After `openapi.json` and fixtures are frozen, Go parity checks must not start
or import the Python server. Artifact refreshes, if ever needed, are separate
maintainer actions. Migration gates run from committed OpenAPI, MCP
transcripts, database fixtures, and Go responses.

## Future Endpoint Parity Checklist

Before a new Go endpoint or MCP tool is considered migrated:

1. Identify the OpenAPI path/method or MCP tool name being ported.
2. Add request fixtures for success, validation failure, auth/tenant behavior
   if applicable, and one representative storage edge case.
3. Add expected response fixtures from the frozen contract.
4. Add a Go test that serves the Go handler/tool against deterministic store
   state and normalized fixture comparison.
5. Document any client-invisible normalization in the fixture metadata.
6. Keep the route feature-flagged until the parity test is green in CI.
