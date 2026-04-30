# Migration Research Synthesis

Status: sf auto execution artifact
Date: 2026-04-29

## Decision Summary

The migration is ready to proceed as a Go-only runtime migration, not a Python
runtime hardening effort. The Python source remains reference history. Go parity
must be proven from committed artifacts: `openapi.json`, HTTP fixtures, MCP
transcripts, deterministic database fixtures, and Go tests.

## Contract

- `openapi.json` is the frozen HTTP contract.
- Go now serves that committed artifact at `GET /openapi.json`.
- Current Go HTTP surface is health, version, OpenAPI, bank list, bank profile,
  bank create/update, and bank delete endpoints.
- Bank success responses now have committed fixture comparisons in
  `go/internal/httpapi/testdata/fixtures/`.
- Future endpoints must add fixtures and normalized comparison tests before
  traffic can flip.
- MCP parity still needs committed `tools/list` and tool-call transcripts
  before Go MCP is enabled as a migrated surface.

See:

- `docs/migration/research/current-contract.md`
- `docs/migration/PARITY_CONTRACT.md`

## Database And Retrieval

The first-cut storage target is external Postgres 18 with VectorChord suite:
`vector`, `vchord`, `pg_tokenizer`, `vchord_bm25`, and `pg_trgm`.

Retrieval semantics remain BM25 + vector + graph/temporal lanes + RRF +
rerank. This migration does not redesign ranking and does not add first-cut
branches for `pg0`, `pgvectorscale`, Apache AGE, or TimescaleDB.

Current Go bank creation now uses storage-profile-aware partial vector-index
SQL. The default `vchord` profile creates `vchordrq (embedding vector_l2_ops)`
indexes per bank and fact type; `pgvector` keeps HNSW only as a compatibility
profile.

See:

- `docs/migration/research/db-and-retrieval.md`
- `docker/postgres-vchord/initdb/001-vectorchord.sql`

## Embedding And Rerank Gateway

The Go runtime calls the llm-gateway OpenAI-compatible `/v1` surface directly:

- `SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1`
- `SINGULARITY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1`
- embeddings: `POST /v1/embeddings`
- rerank: `POST /v1/rerank`

Qwen3-Embedding-4B native 2560D is the preferred production profile. Leave
`SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` unset for native output. Set it only
for measured truncation experiments.

Legacy Go env aliases have been removed. The migration uses the explicit
OpenAI-compatible llm-gateway env names only.

See:

- `docs/migration/research/embedding-gateway.md`
- `go/internal/embed`
- `go/internal/rerank`

## Execution

`docs/migration/GO_CHARM_EXECUTION_PLAN.md` is the operational plan for sf auto.
It keeps Phase 4 blocked until sf persistent-agent APIs are concrete and routes
Phases 0-3/5 through contract, Go scaffold, endpoint parity, worker/admin, and
cutover gates.

`docs/migration/CUTOVER_RUNBOOK.md` defines the endpoint-by-endpoint flip and
rollback rules. Cutover is routing-only unless a separate forward-only
compatibility migration is explicitly approved.

## Remaining Gaps

- Commit remaining HTTP fixtures, including error/storage-edge cases and all
  non-bank routes.
- Commit MCP transcripts.
- Add retain/recall Go integration fixtures against PG18 VectorChord.
- Add held-out recall evals and baseline thresholds.
- Decide the final admin TUI stack details after worker parity starts.
