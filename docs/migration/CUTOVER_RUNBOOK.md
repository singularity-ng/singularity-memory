# Cutover Runbook

Status: sf auto execution artifact
Date: 2026-04-29

## Purpose

This runbook controls production traffic movement from the previous serving
artifact to `singularity-memory-go`. Cutover is reversible at every endpoint
group and never changes existing memory content as part of the traffic flip.

## Runtime Contract

Production target:

- Service: `singularity-memory-go`
- Database: external Postgres 18 with VectorChord suite
- Storage profile: `SINGULARITY_STORAGE_PROFILE=vchord`
- Vector extension: `SINGULARITY_VECTOR_EXTENSION=vchord`
- Text extension: `SINGULARITY_TEXT_SEARCH_EXTENSION=vchord`
- MCP: `SINGULARITY_MCP_ENABLED=true`

Required inference configuration:

```text
SINGULARITY_EMBEDDINGS_PROVIDER=openai
SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1
SINGULARITY_EMBEDDINGS_OPENAI_MODEL=qwen/qwen3-embedding-4b
SINGULARITY_EMBEDDINGS_OPENAI_API_KEY=<secret>
SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS=
SINGULARITY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1
SINGULARITY_RERANK_MODEL=qwen/qwen3-reranker-4b
```

Leave `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` empty for native 2560D Qwen3
embeddings unless the migration has recorded measured fallback evidence.

## Pre-Cutover Checklist

- `openapi.json`, HTTP fixtures, and MCP transcripts are committed and frozen.
- Go contract tests pass without starting the Python server.
- Go integration tests run against Postgres 18 VectorChord.
- `docker-compose.yaml` starts `singularity-memory-postgres` from
  `ghcr.io/tensorchord/vchord-suite:pg18-latest`.
- CI `test-go` passes with Go 1.23 and PG18 VectorChord.
- `GET /healthz` and `GET /v1/banks` pass against the candidate Go service.
- Retain and recall parity fixtures pass.
- Recall evals meet the frozen baseline.
- MCP replay passes against the Go `/mcp/` endpoint.
- Existing clients need no config changes beyond the server URL:
  Python client, generated OpenAPI client, Hermes, OpenClaw, Claude Code,
  Cursor, Windsurf, and OpenCode.
- Rollback route for the endpoint group is known and tested.

## Local Candidate Smoke

Use the compose stack before any environment promotion:

```bash
docker compose build singularity-memory-postgres singularity-memory
docker compose up -d singularity-memory-postgres singularity-memory
curl -fsS http://127.0.0.1:8888/healthz
curl -fsS http://127.0.0.1:8888/v1/banks
```

Expected result:

- Postgres health check is green.
- Go service starts with `SINGULARITY_FEATURE_BANKS=true`.
- Health reports service and database readiness.
- Bank endpoint returns the frozen-compatible response shape.

## Endpoint Flip Order

Flip one group at a time:

1. Health, version, and bank read endpoints.
2. Bank write endpoints.
3. Retain write path.
4. Recall read path.
5. Entities, documents, mental models, directives, and anti-patterns.
6. Audit, operations, files, and webhooks.
7. MCP `/mcp/`.
8. Worker and admin traffic.

Each group must pass:

- Fixture parity before the flip.
- Live smoke immediately after the flip.
- Error-rate and latency watch during the soak.
- Rollback drill before the next group starts.

## Cutover Steps Per Endpoint Group

1. Confirm the candidate Go artifact matches the commit that passed CI.
2. Confirm database target is the existing Postgres 18 VectorChord database.
3. Confirm no schema migration mutates existing memory content.
4. Start Go service with the production `SINGULARITY_*` environment.
5. Run health and endpoint smoke checks against Go directly.
6. Route only the selected endpoint group to Go at the reverse proxy or service
   routing layer.
7. Run the endpoint fixture smoke against the public route.
8. Watch logs, metrics, error rate, and latency.
9. Keep the previous serving artifact available for rollback.
10. Record the flip time, commit, route group, and operator.

Minimum soak:

- Small read endpoints: one week of green CI and clean live metrics.
- Retain: one week after live write verification.
- Recall: one week plus eval confirmation and reranker latency review.
- Full Go serving before archive: four weeks of clean operations.

## Rollback Steps Per Endpoint Group

Rollback is routing-only unless a forward-only schema migration has been
explicitly approved for compatibility work.

1. Stop sending the affected endpoint group to Go.
2. Route the group back to the previous serving artifact.
3. Keep Go running for diagnostics unless it is causing shared resource impact.
4. Confirm public smoke checks pass through the previous artifact.
5. Check that no queued operations are stuck or duplicated.
6. Capture Go logs, metrics, failing fixture output, and the candidate commit.
7. Mark the endpoint group blocked and return to migration reassess.

Rollback must not:

- Rewrite memory content.
- Regenerate or weaken frozen fixtures.
- Switch embedding dimensions to hide a retrieval failure.
- Attempt Phase 4 agent behavior as a mitigation.

## Database Safety

- Use the existing database and schema.
- New migrations, if unavoidable, must be forward-only and compatibility-safe.
- Do not alter existing Alembic migrations.
- Do not introduce `pg0`, Apache AGE, TimescaleDB, or `pgvectorscale` as
  production migration dependencies.
- Verify required extensions are present before serving DB-backed endpoints:
  `vector`, `vchord`, `pg_tokenizer`, `vchord_bm25`, and `pg_trgm`.
- Reset test fixtures between parity runs; never reset production data during
  cutover.

## llm-gateway Safety

- Embeddings and rerank use the same llm-gateway `/v1` domain.
- The Go client must avoid double-prefixing base URLs as `/v1/v1/...`.
- Embedding requests use batched `input` arrays.
- Provider response order is restored by index where needed.
- Vector-count mismatch is a hard failure.
- Native 2560D Qwen3 embeddings are the default for the `vchord` profile.

## Phase 4 Hold

Phase 4 is not part of production cutover unless sf persistent-agent APIs are
already stable.

Cutover may proceed from Phase 3 to Phase 5 while Phase 4 remains blocked.
Do not add central-agent traffic, speculative inbox tables, or fantasy-hosted
production behavior during the Go runtime cutover.

## Final Archive Criteria

Archive planning for the previous serving artifact can start only after:

- All HTTP and MCP routes have served from Go for at least four clean weeks.
- Retain, recall, MCP, worker, and admin gates remain green.
- Rollback has been tested and documented.
- Python source has been kept as historical reference for one release cycle.
- A separate removal plan exists for any source deletion or package cleanup.
