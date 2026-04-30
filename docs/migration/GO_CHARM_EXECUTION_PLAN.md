# Go Charm Execution Plan

Status: sf auto execution artifact
Date: 2026-04-29

## Scope

This plan migrates Singularity Memory to the Go runtime in `go/` while keeping
the existing HTTP, MCP, database, and client contracts stable. The Python
server remains reference material only. New serving work targets the Go binary
`cmd/singularity-memory-go` and the Charm-aligned runtime described in
`MIGRATION.md`.

Hard constraints:

- Go runtime only for migration work. Do not revive or harden the Python
  runtime as a deployment target.
- Freeze HTTP fixtures, MCP transcripts, and `openapi.json` before endpoint
  parity work. Go responses are compared against frozen artifacts without a
  live Python server.
- Use external Postgres 18 with the VectorChord suite only:
  `vector`, `vchord`, `pg_tokenizer`, `vchord_bm25`, and `pg_trgm`.
- Use the llm-gateway OpenAI-compatible domain for the full inference surface:
  `https://llm-gateway.centralcloud.com/v1` for both `/v1/embeddings` and
  `/v1/rerank`.
- Keep Phase 4 blocked until sf persistent-agent and inter-agent messaging APIs
  are concrete enough to bind against.
- Cutover is endpoint-gated and reversible. No production traffic flips until
  the relevant parity, recall, and soak gates pass.

Current implemented slice:

- Health, version, and `GET /openapi.json`.
- Bank list, bank profile, bank create/update, and bank delete under
  `/v1/default/banks`.
- Go config with `vchord` as the default storage profile and explicit
  llm-gateway OpenAI-compatible env names only.
- Batched embedding and rerank clients for the llm-gateway `/v1` surface.
- Storage-profile-aware bank vector index SQL. The default `vchord` profile
  creates `vchordrq (embedding vector_l2_ops)` partial indexes per bank and
  fact type.
- Success fixtures for bank list/profile/update/delete live under
  `go/internal/httpapi/testdata/fixtures/` and are compared from Go tests.

## Repo Layout

Use the existing in-repo Go layout:

```text
go/
  cmd/singularity-memory-go/      # target Go server binary
  ...                             # Go HTTP, MCP, storage, retrieval, worker code
docker/postgres-vchord/           # PG18 VectorChord image layering
docker/singularity-memory-go/     # Go service image
docs/migration/                   # migration plans, runbooks, research, contracts
openapi.json                      # committed frozen HTTP contract
```

Do not split to a second repository. The current `README.md`, `go/README.md`,
`docker-compose.yaml`, and CI workflow already assume `go/` is the target
runtime and build/test path.

## Dependency Graph

```text
P0 contract freeze
  -> P1 Go scaffold and config
    -> P1 bank endpoint parity
      -> P2 endpoint parity lanes
        -> P2 retain write path
          -> P2 recall read path and retrieval evals
            -> P3 worker/admin parity
              -> P5 production cutover and archive plan

P1 inference client
  -> P2 retain
  -> P2 recall

P1 storage profile
  -> all DB-backed endpoints
  -> worker/admin parity

P4 fantasy central-agent host
  waits on sf persistent-agent APIs and is not on the P0-P3/P5 critical path
```

Parallelizable units:

- Contract research, DB/retrieval research, embedding-gateway research, and
  deployment/rollback research can run in parallel.
- After the shared router/config/storage foundations land, independent small
  endpoint groups can run in parallel when their write sets do not overlap.
- Admin UI view work can run beside worker tests after the operations schema
  contract is frozen.

Serial gates:

- No endpoint port starts before the frozen contract artifacts exist.
- No retain/recall port starts before the batched embedding client and PG18
  VectorChord storage profile are tested.
- No recall traffic flips before fixture parity and held-out recall evals pass.
- No cutover starts before every endpoint has soaked on Go.

## Phase 0 - Contract Freeze

Goal: make Python unnecessary for parity verification.

Work:

- Commit and serve `openapi.json` from the Go service as the frozen HTTP
  contract artifact.
- Record HTTP fixtures for all public routes, including banks, retain, recall,
  anti-patterns, webhooks, admin routes, and health/version routes that clients
  depend on.
- Record MCP JSON-RPC transcripts for the configured `/mcp/` surface used by
  Claude Code, Cursor, Windsurf, OpenCode, Hermes, and OpenClaw.
- Normalize fixtures before comparison: stable key order, stable timestamp
  representation, stable UUID placeholders where IDs are generated, stable
  floating-point/vector formatting, and explicit omission/null handling.
- Create resettable DB fixtures with deterministic banks, memories, links,
  operations, and retrieval examples.

Verification:

```bash
cd go && go test ./...
cd go && go build ./...
docker compose build singularity-memory-postgres singularity-memory
docker compose up -d singularity-memory-postgres
```

Gate:

- CI fails when Go responses drift from frozen OpenAPI or fixtures.
- CI can run contract tests without starting the Python server.
- Fixture reset is deterministic between tests.

## Phase 1 - Go Charm Runtime Foundation

Goal: make the Go service the only active migration runtime.

Work:

- Keep `cmd/singularity-memory-go` as the server binary.
- Use the explicit `SINGULARITY_*` configuration names from `README.md` and
  `go/README.md`; do not add legacy env aliases.
- Use `pgx` against the existing database schema. Do not mutate existing memory
  content.
- Keep `SINGULARITY_STORAGE_PROFILE=vchord`,
  `SINGULARITY_VECTOR_EXTENSION=vchord`, and
  `SINGULARITY_TEXT_SEARCH_EXTENSION=vchord`.
- Implement the first end-to-end endpoint as `GET /v1/banks` and
  `GET /v1/default/banks`; this is the smallest DB-backed compatibility slice
  and is already the current Go scaffold boundary.
- Implement bank profile/create/update/delete as the next low-risk DB-backed
  slice before retain and recall.
- Add the direct llm-gateway client:
  - `SINGULARITY_EMBEDDINGS_PROVIDER=openai`
  - `SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1`
  - `SINGULARITY_EMBEDDINGS_OPENAI_MODEL=qwen/qwen3-embedding-4b`
  - `SINGULARITY_EMBEDDINGS_OPENAI_API_KEY`
  - `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS`
  - `SINGULARITY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1`
  - `SINGULARITY_RERANK_MODEL=qwen/qwen3-reranker-4b`
- Accept either root or `/v1` base URLs without producing `/v1/v1/...`.
- Send embeddings as batched `input` arrays, preserve response order by index,
  and fail on vector-count mismatch.
- Leave `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` unset for native Qwen3
  2560-dimensional output unless a measured truncation experiment proves a
  better production choice.

Verification:

```bash
cd go && go test ./...
cd go && go build ./cmd/singularity-memory-go
docker compose up -d singularity-memory-postgres singularity-memory
curl -fsS http://127.0.0.1:8888/healthz
curl -fsS http://127.0.0.1:8888/v1/banks
```

Gate:

- Static Go binary builds.
- Health check proves Postgres connectivity.
- Bank responses match frozen fixtures.
- New-bank storage setup creates `vchordrq` vector indexes when
  `SINGULARITY_STORAGE_PROFILE=vchord`.
- Embedding/rerank client tests cover batching, dimensions, base URL joining,
  response ordering, and mismatch failure.

## Phase 2 - Endpoint Parity

Goal: port HTTP and MCP behavior endpoint by endpoint without client-visible
contract drift.

Order:

1. Bank profile, create/update, and delete endpoints under
   `/v1/default/banks/{bank_id}` and `/profile`.
2. `POST /v1/default/banks/{bank_id}/memories` retain write path.
3. `POST /v1/default/banks/{bank_id}/memories/recall` recall read path:
   BM25, vector, RRF, and rerank parity.
4. Entities, documents, mental models, directives, and anti-patterns.
5. Audit, operations, files, and webhooks.
6. MCP HTTP wire protocol.

Rules per endpoint:

- Add Go implementation behind a feature flag or route gate.
- Run byte-equivalent fixture comparison after normalization.
- Document any allowed normalization as client-invisible.
- Keep Python-derived behavior as contract history, not a live dependency.
- Do not redesign retrieval during the port.

Recall-specific gate:

- Use the frozen DB fixtures plus held-out retrieval evals.
- Prove recall@k is at or above the frozen baseline.
- Prove reranker latency has no critical regression.
- Verify native 2560-dimensional Qwen3 embeddings against PG18 `vchordrq`.
  Benchmark 1024, 1536, and 2000 dimensions only as fallback evidence; do not
  switch dimensions without measured quality and latency justification.

Verification:

```bash
cd go && go test ./...
cd go && go build ./cmd/singularity-memory-go
docker compose up -d singularity-memory-postgres singularity-memory
```

Add endpoint-specific fixture and eval commands as they land.

Gate:

- Every ported endpoint passes fixture parity.
- MCP transcripts replay successfully against Go.
- Existing Python client, generated OpenAPI client, Hermes, OpenClaw, and MCP
  recipes require no client-side changes.

## Phase 3 - Worker And Admin

Goal: move non-request serving responsibilities to Go.

Work:

- Port background operation processing using the existing database queue and
  schema.
- Process existing queued operations without data rewrites.
- Add an SSH-served admin surface aligned with Charm patterns, using Wish and
  Bubble Tea-compatible components as appropriate.
- Admin must inspect banks, memories, operations, and retrieval state.
- Destructive admin actions require confirmation and emit audit entries.

Verification:

```bash
cd go && go test ./...
cd go && go build ./cmd/singularity-memory-go
docker compose up -d singularity-memory-postgres singularity-memory
```

Gate:

- Worker drains fixture queues correctly.
- Admin read paths work against PG18 VectorChord.
- Destructive paths are confirmed and audited.

## Phase 4 - Blocked Fantasy Central-Agent Host

Goal: eventually host sf central persistent agents on Singularity Memory.

Blocking condition:

- Do not implement this phase until sf SPEC §17 persistent agents and SPEC §18
  inter-agent messaging expose stable concrete APIs, schemas, and tool contracts
  that the Go service can bind to.

Allowed before unblock:

- Keep `fantasy` as a pinned dependency only if needed for compile-time
  scaffolding.
- Record expected integration seams in docs.

Not allowed before unblock:

- No production central-agent behavior.
- No invented inbox schema.
- No traffic or schema dependency on speculative sf APIs.

## Phase 5 - Cutover And Archive

Goal: serve production from Go and retire the prior serving artifact.

Work:

- Flip traffic one endpoint group at a time after green parity and soak.
- Keep rollback routing for every flip.
- Run Go for at least four weeks of clean operations before archiving the old
  runtime path.
- Keep Python source as historical reference for one release cycle after full
  Go cutover, then open a separate removal plan.

Verification:

```bash
cd go && go test ./...
cd go && go build ./cmd/singularity-memory-go
docker compose up -d singularity-memory-postgres singularity-memory
curl -fsS http://127.0.0.1:8888/healthz
```

Gate:

- All public HTTP and MCP routes are served by Go.
- Recall evals and contract fixtures remain green.
- Operations metrics and logs show clean behavior through the soak window.
- Rollback procedure in `docs/migration/CUTOVER_RUNBOOK.md` has been tested.

## Resume Rules For sf auto

- Commit after each green unit with a `migration:` prefix.
- Resume from the last green gate, not from the start.
- If a fixture, eval, or CI gate fails, stop and reassess. Do not weaken the
  frozen contract to make the port pass.
- If parallel work changes shared files, re-read before editing and preserve
  edits made by others.
- If Phase 4 is still blocked, skip it and continue to Phase 5 only after
  P0-P3 gates are green.
