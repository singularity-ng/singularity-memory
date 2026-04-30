# Singularity Memory — Migration Plan: Python → Go on Charm

**Status:** sf-auto execution plan (staged; gates required)
**Date:** 2026-04-29

## Goal

Re-platform Singularity Memory from the current **Python + FastAPI + Postgres-backed memory** codebase to **Go + Charm stack** (`charm` patterns for auth/identity, `fantasy` for the agent runtime), while preserving data, MCP/HTTP wire contract, and downstream-client compatibility.

The trigger is not to harden or revive the Python runtime. Treat the Python
source as implementation reference and contract history only. The target
runtime is Go.

- **Persistent agents are trending central** (sf SPEC §17 NEW). When that lands, central agents need an agent-runtime host. Singularity Memory is the natural home — it's already the cross-instance shared service.
- **`fantasy`** is Charm's Go agent runtime (multi-provider, tools, multi-turn). Building Singularity Memory on it from day one means central agents drop in cleanly later. Retrofitting fantasy after we've built every LLM call site on raw SDKs is real refactor cost.
- **Single static Go binary** is operationally simpler than a Python uv/venv + Alembic + worker stack on each deployment host (laptop, `mikki-bunker`, `aidev`).
- **Charm ecosystem alignment** — sf is moving toward parallel-build of Go services (sf-worker, flight recorder, Charm TUI client). One language for the service tier reduces cognitive load.

This document is the staged migration plan and the automation handoff for `sf`.
`sf auto` may execute Phases 0-3 and Phase 5 as one orchestrated session if
each gate below passes. Phase 4 is the only intentionally conditional phase:
it lands after the sf persistent-agent implementation is present enough to
bind against.

## Non-goals

- **Not** a re-design of the retrieval pipeline. BM25 + vector + RRF + rerank stays as-is in semantics. Implementation moves to Go but the recall contract is preserved.
- **Not** a database swap. The Go migration target is external Postgres 18
  with the VectorChord suite (`vchord`, `pg_tokenizer`, `vchord_bm25`, and the
  pgvector `vector` type that VectorChord uses). Embedded `pg0` is not a Go
  migration target. Data is zero-touch through the migration.
- **Not** a wire-contract break. The HTTP API and MCP protocol are preserved exactly so existing clients (sf, Hermes, OpenClaw, Claude Code, Cursor) keep working unchanged.
- **Not** a rewrite of `extensions/` clients (Python Hermes adapter, TS OpenClaw adapter). They talk over HTTP — server language is invisible to them.

## Current state (as of `0ee7b2f` HEAD)

| Component | Stack | LOC (approx) |
|---|---|---|
| Server (HTTP + MCP + worker) | Python 3.x, FastAPI | ~100k LOC |
| Storage | External Postgres 18 + VectorChord suite (`vchord`, `pg_tokenizer`, `vchord_bm25`, `vector`) | schema |
| Retrieval | `vchord_bm25` + `vchordrq` vector + RRF fusion + reranker | in-server |
| Migrations | Alembic | versioned |
| API contract | OpenAPI frozen from existing code and fixtures | committed as `openapi.json` in the migration working tree |
| Clients | Python (`singularity_memory_client`), generated OpenAPI client, TS via OpenClaw extension | working |
| Deployment | `docker-compose` (`singularity-memory-postgres` + `singularity-memory`) | working |

## Target architecture

```
[singularity-memory-go]                                 single static Go binary
  ├── fang (CLI)                                        charm.land/x/exp scaffolding
  ├── auth/identity layer                               charm-server-style:
  │     SSH-key identity, JWT issuance, encrypted KV    SSH-key based, multi-device sync
  │     for user prefs, optional ed25519 key sync       (port pattern from `charmbracelet/charm`)
  ├── inference-fabric client                           OpenAI-compatible local gateway
  │     ├── /v1/embeddings                              batched Qwen embeddings, optional dimensions
  │     └── /v1/rerank                                  Qwen reranker, same gateway surface
  ├── fantasy agent runtime                             future central-agent host + LLM tools
  │     └── future: central persistent agents host      sf SPEC §17, when wired
  ├── retrieval layer                                   logic ported from existing engine
  │     ├── BM25 (vchord_bm25)
  │     ├── vector (vchordrq over pgvector-compatible vector type)
  │     ├── RRF fusion
  │     └── reranker (inference-fabric → /v1/rerank)
  ├── agent-brain layer                                 native server primitives
  │     ├── brain pages/sources                         agent-readable knowledge pages
  │     ├── links/backlinks/timeline                    graph and provenance surfaces
  │     ├── raw data + page versions                    import/history retention
  │     └── brain jobs                                  durable queued maintenance work
  ├── HTTP server (`net/http` + chi or echo)            same routes, same shapes
  │     /v1/banks, /v1/default/banks/{bank_id}/memories, /openapi.json, …
  ├── MCP server                                        same wire protocol
  ├── webhooks
  ├── admin UI (optional)                               Wish + Bubble Tea served over SSH
  └── observability                                     prometheus via `promwish` patterns
        │
        │ pgx / pq
        ▼
[Postgres 18 + VectorChord suite]                       unchanged data, profile-selected extension
```

**Why each piece:**

| Piece | Reason |
|---|---|
| `fang` (CLI scaffold) | Charm's CLI starter kit — gives us `singularity-memory serve\|mcp\|status` for free. |
| `charm-server`-style auth | SSH-key identity is the right shape for tailnet deployment (Headscale). Encrypted KV for user prefs / config keeps the multi-device sync story without rebuilding. |
| `inference-fabric` direct client | This is the local OpenAI-compatible gateway already built for Qwen embeddings and reranking. The Go server should call it directly; do not add LiteLLM as a migration dependency. |
| `fantasy` | Agent-runtime foundation for central persistent agents and future tool-using workflows. It is not the embedding gateway. |
| `pgx` for Postgres | Most mature Go Postgres driver; SQL/index behavior stays profile-driven against Postgres 18. |
| VectorChord storage | Production and development use Postgres 18 + `vchord` / `vchord_bm25`. The migration does not carry embedded `pg0` or pgvector/native fallback as parity gates. |
| Agent-brain layer | Singularity Memory stores pages, sources, backlinks, timelines, raw/version history, and durable jobs as native server primitives. External brain data is ingested, normalized, and then treated as Singularity Memory data. |
| `wish` + `promwish` | SSH-served admin surface with built-in Prometheus metrics. |
| `pony` + `ultraviolet` (admin UI) | Declarative type-safe TUI markup. Experimental but the Phase-3 admin surface is the right place for the bet — admin tolerates churn better than user-facing TUIs. |
| Other `x/*` packages | `mosaic` (image rendering), `vcr` (session recording / audit), `editor` (inline editing), `input`, `cellbuf`, `vt`, `ansi`, `term` — adopt as fits. The strategic position is comprehensive Charm-ecosystem adoption for new Go services, not piecemeal. |

## sf auto execution contract

This section exists so Singularity Forge can run the migration as a single
autopilot goal without losing the invariants that make the migration safe.

**Copy-paste sf goal:**

```text
Run sf's strategic-planning flow for singularity-memory/MIGRATION.md. Use
parallel swarm research and `subagent({ mode: "debate", rounds: 3, tasks:
[...] })` before locking the plan, then execute the resulting plan through
Phases 0, 1, 2, 3, and 5. Keep Phase 4 blocked unless sf SPEC §17 persistent
agents and SPEC §18 inter-agent messaging have concrete implementation APIs to
bind against. Preserve the committed HTTP and MCP wire contracts, keep existing
Postgres data zero-touch, make Postgres 18 + `vchord` / `vchord_bm25` the only
Go migration storage target, do not spend effort making the Python runtime
work, and do not flip production traffic until every phase gate in MIGRATION.md
passes.
```

**Meaning of "100% in one go":**

- One `sf auto` session may own the whole migration, create units, dispatch
  workers, and continue until the final gate passes.
- The session must still stop at any red gate. "One go" means one orchestrated
  run with checkpoints, not bypassing tests, soak windows, or rollback safety.
- Every unit below must finish with code, tests, docs, and a passing gate before
  dependent units start.
- Do not spend migration effort making the Python runtime deployable. If an
  existing Python-derived artifact is needed, freeze it once and then drive Go
  from committed OpenAPI, fixtures, database schema, and docs.

### sf strategic-planning flow

sf should not blindly execute this table. It should use its normal strategic
planning flow first, then turn that plan into units. The static backlog below
is the minimum decomposition and the safety contract; sf may split units more
finely when that improves parallelism or review quality.

**Provider/model execution constraint:**

The sf run must honor the active sf provider policy instead of assuming
Anthropic or OpenAI availability. On 2026-04-29, the global sf provider
allowlist is:

- `kimi-coding`
- `minimax`
- `zai`
- `mistral`
- `ollama-cloud`
- `alibaba-coding-plan`
- `xiaomi`
- `opencode-go`
- `openrouter` (OpenRouter.ai, `:free` models only via `provider_model_allow`)

`provider_preference` may mention providers outside this list, but only
`allowed_providers` is the hard gate. The run must resolve planning,
research, execution, review, and debate models from this allowlist, apply
`provider_model_allow` (currently restricting `minimax` to `MiniMax-M2.7` /
`MiniMax-M2.7-highspeed`, and `openrouter` to `:free` model IDs only), and
fail early with a provider-readiness note if the required roles cannot be
resolved. The standard `opencode` provider is intentionally not in the hard
allowlist because the current SOPS keys authenticate `opencode-go`; standard
OpenCode free models work unauthenticated today but are not a stable sf
provider path. The Go service runtime must not bake this sf operator allowlist
into Singularity Memory defaults. Runtime embeddings/reranking route through
the local OpenAI-compatible inference-fabric gateway; `fantasy` remains the
agent-runtime/LLM-tool foundation.

Provider credentials must come from the sf-scoped SOPS namespace only. The
`~/.local/bin/sf` wrapper clears inherited provider env vars and exports
`sf.env` plus `sf.providers.<provider>.env` from
`~/.dotfiles/secrets/api-keys.yaml`; it must not fall back to top-level
`google:`, `gemini:`, `openrouter:`, or other global sections. On 2026-04-29,
`sf.env` contains keys for `kimi-coding`, `minimax`, `mistral`, `ollama-cloud`,
`opencode-go`, `openrouter`, `xiaomi`, `zai`, Tavily, and Telegram.
`ALIBABA_API_KEY` and any Google key must be added explicitly under the
sf-scoped namespace before those providers can be used. For Google, live
`models.dev` metadata lists `GOOGLE_GENERATIVE_AI_API_KEY` and `GEMINI_API_KEY`;
sf/pi-ai direct `google` and `google_search` accept both and prefer
`GEMINI_API_KEY` if both are set.

Provider smoke checks on 2026-04-29: the sf-scoped OpenRouter key validates via
`/api/v1/key`; `openai/gpt-oss-20b:free` returned a successful chat completion
and OpenRouter usage stayed `0`. `qwen/qwen3-coder:free` returned an upstream
429 from its backing provider, so sf should treat individual `:free` failures as
provider-rate-limit fallback events rather than key failures. sf's search path
for this migration remains its own `search-the-web`, `search_and_read`,
`fetch_page`, and `google_search` tools.

**Runtime embedding and vector-profile policy:**

- The Go service must call inference-fabric directly through its
  OpenAI-compatible `POST /v1/embeddings` endpoint. Do not introduce LiteLLM
  for this migration.
- Embedding requests must support batched `input` arrays. The gateway already
  supports batching and records embedding output dimensions.
- The Go retain path must preserve the existing engine's two-level batching semantics:
  split large retain requests by `SINGULARITY_RETAIN_BATCH_TOKENS`, then send
  each sub-batch to the embedding backend as one ordered list. The embedding
  client may internally split that list by provider `batch_size`, but every
  provider request must use a batched `input` array, restore provider response
  order by index when needed, return the final vector list in the same order as
  the input texts, and fail on vector-count mismatch instead of silently
  dropping rows.
- Qwen3-Embedding-4B is a native 2560-dimensional model. The storage profile
  is `vchordrq`, so benchmark and prefer native 2560 first. Also benchmark
  1024, 1536, and 2000 so the plan has a measured fallback if native 2560 is
  slower or lower quality for our workload.
- `halfvec` is not part of the first migration target. The existing engine
  uses `Vector(dim)` and vector/vchord indexes, not `halfvec`; adding `halfvec`
  would be a separate storage optimization.
- `pgvectorscale` is not a migration target. Existing compatibility for
  it may stay, but M001-M003 should plan around `vchord`/`vchord_bm25`.
- Embedded DB policy: skip `pg0` in the Go migration. Agents need networked
  inference, so an offline embedded database is not a first-cut requirement.
  Local/dev uses the layered Postgres 18 VectorChord container instead.
- Apache AGE and TimescaleDB policy: do not include them in the first Go
  migration. Current graph retrieval is relational link expansion, not
  openCypher. TimescaleDB may be useful later for operations/metrics
  hypertables, but it is not required for retain/recall parity.
- Required runtime env shape: use only the explicit llm-gateway OpenAI-compatible
  names in the Go runtime: `SINGULARITY_EMBEDDINGS_PROVIDER=openai`,
  `SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL`, `SINGULARITY_EMBEDDINGS_OPENAI_MODEL`,
  `SINGULARITY_EMBEDDINGS_OPENAI_API_KEY`, and
  `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS`. Leave
  `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` unset for native Qwen3-Embedding-4B
  2560D output; set it only for measured truncation experiments.
- Reranking uses the same llm-gateway `/v1` OpenAI-compatible surface via
  `SINGULARITY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1`
  and `POST /v1/rerank`. Embeddings use
  `SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1`
  and `POST /v1/embeddings`. Gateway base URLs may be configured either as the
  root or the v1 base; the Go client must not produce `/v1/v1/...`.

**Current Go implementation snapshot (2026-04-30):**

- `go/` builds and tests as the active migration runtime.
- `GET /openapi.json` serves the committed root `openapi.json`.
- Config defaults to `SINGULARITY_STORAGE_PROFILE=vchord`,
  Qwen3-Embedding-4B, native dimensions by omission, and the explicit
  llm-gateway `/v1` env names. Legacy embed/rerank env aliases are removed.
- The embedding client batches OpenAI-compatible `/v1/embeddings` inputs,
  preserves response order by index, serializes `dimensions` only when set, and
  fails vector-count mismatches.
- The rerank client uses the same base-URL handling for `/v1/rerank`.
- Current Go HTTP parity surface is health, version, OpenAPI, bank list, bank
  profile, bank create/update, and bank delete.
- Bank list/profile/update/delete success responses have committed fixture
  comparisons under `go/internal/httpapi/testdata/fixtures/`.
- Bank creation uses storage-profile-aware vector index SQL. The default
  `vchord` profile creates per-bank/per-fact-type `vchordrq` indexes with
  `vector_l2_ops`; `pgvector` remains a non-production compatibility profile
  that uses HNSW.
- Compose and CI point at the PG18 VectorChord image and required extensions.

**Required planning inputs:**

- `MIGRATION.md` in this repo.
- `TODO.md` in this repo, especially P0 items 1-3.
- `README.md` for current product boundaries and client commitments.
- sf `SPEC.md` sections 16, 17, and 18.
- sf ADR-012, ADR-013, ADR-014, ADR-016, and ADR-017.
- sf `BUILD_PLAN.md` rows that mention Singularity Memory or Charm services.

**Required planning output before implementation:**

sf must create an execution plan artifact in the working tree before code
changes start:

```text
docs/migration/GO_CHARM_EXECUTION_PLAN.md
```

That artifact must include:

1. The selected repo layout (`go/` in this repo unless the plan justifies a
   split repo).
2. A dependency graph for the migration units.
3. Which units can run in parallel and which are serial gates.
4. The exact verification command list per unit.
5. The compatibility contract for HTTP, MCP, DB schema, and existing clients.
6. The rollback story for every traffic flip.
7. The explicit Phase 4 blocking condition tied to sf persistent-agent APIs.
8. The embedding gateway contract: inference-fabric `/v1/embeddings`, batch
   input shape, optional dimensions behavior, selected Postgres 18
   `vchord`/`vchord_bm25` profile, and the measured dimension choice.

**Planning acceptance gate:**

Implementation may not begin until the execution plan answers these questions:

- What is the first end-to-end Go endpoint and why?
- How is `openapi.json` generated, frozen, and drift-checked?
- How are committed contract fixtures and Go responses normalised before byte comparison?
- How are DB fixtures created and reset between parity tests?
- Which retrieval evals prove recall quality did not regress?
- Which embedding dimension is used for `vchord`, and what proves it works
  with inference-fabric's batched OpenAI-compatible endpoint?
- How does sf resume after a partially completed migration run?
- What production switch flips traffic per endpoint, and how is it rolled back?

If sf cannot answer one of those, it must enter reassess instead of inventing
implementation details ad hoc.

### sf flow adequacy check

The migration depends on sf flows that are real today, not aspirational ones.
Checked against the sf repo on 2026-04-29:

| Needed capability | sf status | Evidence | Migration posture |
|---|---|---|---|
| Milestone research before planning | Good enough | `prompts/research-milestone.md` writes research via `sf_summary_save` and updates `.sf/PM-STRATEGY.md`. | Required before implementation. |
| Strategic milestone planning | Good enough | `prompts/plan-milestone.md` explores code/docs, runs a Vision Alignment Meeting, persists via `sf_plan_milestone`. | Required; this is the main strategic-planning entrypoint. |
| Parallel research swarm | Good enough if parent synthesises | `prompts/parallel-research-slices.md` dispatches one subagent per slice in parallel and verifies research files. | Required for Phase 0 and Phase 1 research lanes. |
| Role-based stakeholder critique | Good enough as debate or parallel batch | `plan-milestone.md` defines PM/User/Customer/Business/Researcher/Delivery/Partner/Combatant/Architect/Moderator lenses. | Required for roadmap critique. |
| Bounded multiagent debate | Good enough | `subagent` supports `mode: "debate"`, bounded `rounds`, and prior-round transcript injection. | Required for high-risk plan critique. |
| Full inbox swarm chat | Not implemented yet | sf SPEC §17-18 still marks `agent_inbox`, `agent_messages`, and `send_message` as NEW. | Do not require it for Phases 0-3/5. |
| Docs rewrite/sync | Good enough | `prompts/rewrite-docs.md`; SPEC hook section describes doc sync after code-mutating phases. | Required before marking migration phases done. |
| Verification and milestone validation | Good enough | `skills/finish-and-verify`; `prompts/validate-milestone.md` dispatches 3 parallel reviewers. | Required for every phase gate. |
| Worktree/parallel execution | Good enough with file-conflict gates | `skills/working-in-parallel`; `slice-parallel-orchestrator.ts`; worktree manager. | Allowed only for units with disjoint write sets. |

**Conclusion:** sf is good enough to drive this migration as a strategic,
research-heavy, documented auto run. It now has bounded multiagent debate for
plan critique. It still does not have full inbox-based swarm chat, so this
migration requires **parallel swarm research + bounded debate + parent
synthesis**, not persistent inter-agent messaging.

### Required swarm research and docs

Before implementation, sf must run a parallel research swarm, then a bounded
debate over the synthesis, and persist the results under:

```text
docs/migration/research/
```

Required lanes:

| Lane | Model tier | Output |
|---|---|---|
| `current-contract` | research | `docs/migration/research/current-contract.md` — HTTP routes, MCP tools, generated client surfaces, client compatibility risks. |
| `db-and-retrieval` | research | `docs/migration/research/db-and-retrieval.md` — schema, Postgres 18 + vchord/vchord_bm25 handling, high-dimensional vector behavior, BM25/vector/RRF/rerank parity risks. |
| `embedding-gateway` | research | `docs/migration/research/embedding-gateway.md` — inference-fabric `/v1/embeddings` and `/v1/rerank` contracts, Qwen3 batching, dimensions/Matryoshka behavior, and production vchord native-2560 choice. |
| `go-charm-stack` | research | `docs/migration/research/go-charm-stack.md` — `fang`, `wish`, `promwish`, `fantasy`, `catwalk`, `pgx`, version pins, churn risks. |
| `parity-and-evals` | research | `docs/migration/research/parity-and-evals.md` — fixture strategy, byte comparison rules, recall@k eval strategy. |
| `deployment-and-rollback` | research | `docs/migration/research/deployment-and-rollback.md` — reverse proxy flips, feature flags, one-command rollback, tailnet placement. |
| `sf-integration` | research | `docs/migration/research/sf-integration.md` — sf SPEC §16-18 compatibility, generated Go client needs, Phase 4 blocking APIs. |

Then the parent planner must write:

```text
docs/migration/RESEARCH_SYNTHESIS.md
docs/migration/DEBATE_SYNTHESIS.md
docs/migration/GO_CHARM_EXECUTION_PLAN.md
docs/migration/PARITY_CONTRACT.md
docs/migration/CUTOVER_RUNBOOK.md
```

No production code may be written until those docs exist. The debate must use
`subagent({ mode: "debate", rounds: 3, tasks: [...] })` with at least these
lenses: advocate, challenger, architect, delivery lead, and operations. If sf's
research swarm or debate cannot produce a high-confidence answer for any lane,
the migration must enter reassess and either narrow scope or add a prerequisite
unit.

**Global rules for the sf run:**

1. Create a migration branch before edits: `migration/go-charm`.
2. Treat Python as reference source only. Do not add work whose purpose is
   making the Python runtime deployable.
3. Never alter existing Alembic migrations except by adding new forward-only
   migrations that are required by compatibility work.
4. Do not change the committed `openapi.json` manually after it is frozen.
   Regenerate it from Go only after parity against the frozen contract is
   proven.
5. Feature-flag every Go-served endpoint until the endpoint's parity gate
   passes.
6. Preserve all existing clients: Python client, generated OpenAPI client,
   Hermes, OpenClaw, MCP recipes, Claude Code, Cursor, Windsurf, OpenCode.
7. No retrieval redesign during this migration. If an implementation mismatch
   tempts a retrieval change, open a separate ADR and keep this migration
   blocked.
8. Commit after each green unit with a message beginning `migration:` so sf can
   resume from clean checkpoints.

**Repository readiness gate before Phase 1:**

- `openapi.json` exists at repo root as the frozen HTTP contract.
- Contract tests fail when Go responses drift from `openapi.json` or frozen
  fixtures.
- At least one Go integration smoke verifies Postgres 18 VectorChord extension
  availability.
- Retain/recall integration fixtures are required before SM-MIG-021 and
  SM-MIG-022 start; they are not a precondition for the Phase 1 scaffold.
- CI runs the OpenAPI/fixture contract tests and the Postgres extension smoke
  when a database service is available.
- `README.md` or `TODO.md` links to this migration plan.

**Phase gates:**

| Gate | Required evidence |
|---|---|
| G0 Contract frozen | `openapi.json`, MCP transcripts, and HTTP fixtures are committed; CI can validate Go against them without requiring a live Python server. |
| G1 Go scaffold alive | `go/` builds a static binary; `singularity-memory-go --help` works; `GET /healthz` and `GET /v1/banks` work against test Postgres. |
| G2 Endpoint parity | For every ported endpoint, Go returns byte-equivalent JSON against frozen fixtures, or a documented, client-invisible normalisation explains the difference. |
| G3 Recall quality | Held-out evals in `eval-workspace/` meet the frozen baseline and show no critical regression in reranker latency. |
| G4 Worker/admin parity | Go worker processes existing queued operations; admin UI can inspect banks, memory units, operations, and destructive actions require confirmation. |
| G5 Production cutover | All routes served by Go for at least 4 weeks of clean ops, with rollback to the previous deployment artifact documented. |

**sf unit backlog:**

| Unit | Phase | Task | Done when |
|---|---:|---|---|
| SM-MIG-000 | 0 | Add discoverability links from README/TODO to `MIGRATION.md`. | A maintainer can find this plan from repo root docs. |
| SM-MIG-001 | 0 | Freeze the HTTP/MCP contract artifacts. | `openapi.json`, HTTP fixtures, and MCP transcripts exist without requiring a live Python server during Go migration. |
| SM-MIG-002 | 0 | Commit generated `openapi.json`. | File exists at repo root and includes all current HTTP paths. |
| SM-MIG-003 | 0 | Add Go contract tests for `openapi.json` and fixtures. | Tests fail on Go contract drift and pass without starting Python. |
| SM-MIG-004 | 0 | Add Go integration smoke for Postgres 18 VectorChord extensions. | Test opens the configured database and verifies `vector`, `vchord`, `pg_tokenizer`, `vchord_bm25`, and `pg_trgm`. |
| SM-MIG-004B | 0 | Add deterministic retain/recall DB fixtures before porting retain. | Test fixtures exercise Postgres-backed retain and recall end-to-end and are resettable between runs. |
| SM-MIG-005 | 0 | Add CI workflow. | CI runs Go contract tests and the smoke integration test with Postgres 18 + VectorChord. |
| SM-MIG-006 | 0 | Add layered Postgres 18 VectorChord container. | Compose builds a Postgres 18 image from `ghcr.io/tensorchord/vchord-suite:pg18-latest`, enables `vector`, `vchord`, `pg_tokenizer`, `vchord_bm25`, and `pg_trgm`, and the service boots healthy. |
| SM-MIG-010 | 1 | Create `go/` module and CLI skeleton. | `go test ./...` and `go build ./cmd/singularity-memory-go` pass. |
| SM-MIG-011 | 1 | Add config loader matching current `SINGULARITY_*` env names. | Go config accepts current deployment env without client-side changes. |
| SM-MIG-012 | 1 | Add pgx connection and health endpoint. | Go `/healthz` proves DB connectivity in CI. |
| SM-MIG-013 | 1 | Add Go inference-fabric embedding/rerank client. | Batched `/v1/embeddings` calls work against a fake OpenAI-compatible server; provider `batch_size` splitting is covered; input order is preserved; vector-count mismatch fails; optional `dimensions` is serialized only when configured; per-profile dimensions are supported; no LiteLLM dependency is introduced. |
| SM-MIG-014 | 1 | Implement storage profile config for Postgres 18 + `vchord`/`vchord_bm25`. | Go config preserves current env names; vchord is documented/tested as the production profile that may use native 2560-dimensional Qwen3 embeddings; `pg0`, Apache AGE, TimescaleDB, and `pgvectorscale` are not first-cut storage targets. |
| SM-MIG-015 | 1 | Implement `GET /v1/banks` in Go. | Frozen fixture comparison passes for list banks. |
| SM-MIG-020 | 2 | Port bank profile/create/update/delete endpoints under `/v1/default/banks/{bank_id}` and `/profile`. | Per-endpoint parity tests pass. |
| SM-MIG-021 | 2 | Port `POST /v1/default/banks/{bank_id}/memories` retain write path. | Retain fixtures pass and DB writes match the frozen contract semantics. |
| SM-MIG-022 | 2 | Port `POST /v1/default/banks/{bank_id}/memories/recall` recall read path. | Recall JSON parity passes on fixtures; eval recall@k gate passes. |
| SM-MIG-023 | 2 | Port entities/documents/mental-model/directive endpoints. | Per-resource parity tests pass. |
| SM-MIG-024 | 2 | Port audit/operations/files/webhook endpoints. | Per-resource parity tests pass. |
| SM-MIG-025 | 2 | Port MCP HTTP wire. | Recorded MCP sessions replay against Go with equivalent responses to the frozen transcript. |
| SM-MIG-026 | 2 | Add native brain pages, links, timeline, and importer. | Go stores page/source/link/timeline state in Postgres, writes pages into memory units for recall, and imports external brain-page data into the native schema. |
| SM-MIG-027 | 2 | Add durable brain job queue. | Go can enqueue, list, claim, and complete `brain_jobs` so background maintenance is server-owned and inspectable. |
| SM-MIG-030 | 3 | Port background worker execution. | Existing operation queue drains correctly under Go worker in test. |
| SM-MIG-031 | 3 | Build Wish admin shell. | Admin SSH session starts and lists banks/operations. |
| SM-MIG-032 | 3 | Add admin destructive-action confirmations and audit trail. | Destructive admin actions require confirmation and emit audit entries. |
| SM-MIG-040 | 4 | Bind fantasy persistent-agent host. | Blocked until sf persistent-agent APIs exist; do not estimate as part of the first run. |
| SM-MIG-050 | 5 | Flip default serving to Go. | Reverse proxy/config defaults route all endpoints to Go after soak. |
| SM-MIG-051 | 5 | Archive Python runtime. | Python source remains as historical reference for one release cycle, then removal plan is opened. |

**Default verification commands for sf units:**

```bash
go test ./...
go build ./cmd/singularity-memory-go
```

Python tests may remain as historical checks if they already run, but they are
not required migration gates and SF must not add work just to make them pass.
Add or adjust commands per unit as the Go tree lands. If any command requires
external services, the unit must either provide a compose/testcontainers setup
or mark the dependency explicitly in the unit output.

**Blocking conditions:**

- No committed OpenAPI contract.
- No integration test capable of catching retain/recall regressions.
- Any client contract break without a compatibility shim.
- Any data migration that mutates existing memory content.
- Recall eval below the frozen baseline threshold.
- Phase 4 attempted before sf's persistent-agent implementation is real enough
  to expose stable APIs.

## Migration phases

### Phase 0 — preparation (1–2 weeks)

- Commit the current OpenAPI spec to repo root (`openapi.json`) — required as the contract the Go server must satisfy. This is also TODO #3 in the existing `TODO.md`; this migration *forces* it.
- Bring up Go-first contract and integration tests before any port. Without
  integration tests, parity verification during Phase 2 is impossible.
- Set up CI (TODO #2) so the parity gate runs automatically.
- Document the wire contract — every endpoint's request/response shape, every MCP tool's schema. Treat as frozen for the duration of the migration.

### Phase 1 — greenfield Go scaffold (2–3 weeks)

- New top-level `go/` directory in this repo (or a new `singularity-memory-go` repo — decide based on tooling preference).
- Scaffold the Go server: `fang`-based CLI, server skeleton, charm-server-style auth layer, inference-fabric client, fantasy registered for future agent work, MCP server stub, OpenAPI router from the committed spec.
- Add a direct OpenAI-compatible inference-fabric client for batched `/v1/embeddings` and `/v1/rerank`. It must preserve existing `SINGULARITY_EMBEDDINGS_OPENAI_*` config semantics and must not route Qwen through LiteLLM.
- Use one storage profile from the start: external Postgres 18 with `vchord` /
  `vchord_bm25`. Do not make `pg0`, Apache AGE, TimescaleDB, or
  `pgvectorscale` part of the first Go migration target.
- Implement the **first endpoint** end-to-end: `GET /v1/banks` (list banks). Smallest, lowest risk.
- Connect to the same Postgres via `pgx`. No data migration.
- Behind a feature flag until the endpoint passes its frozen-contract gate.
- Add a CI step: hit `GET /v1/banks` against Go in test mode and assert byte-equal responses against frozen fixtures.

### Phase 2 — endpoint parity (4–8 weeks)

- Port endpoints one at a time, in this order (smallest → biggest):
  1. Bank profile and write surface:
     `GET /v1/default/banks/{bank_id}/profile`,
     `PUT /v1/default/banks/{bank_id}`,
     `PATCH /v1/default/banks/{bank_id}`,
     `DELETE /v1/default/banks/{bank_id}`.
  2. `POST /v1/default/banks/{bank_id}/memories` (retain write path).
  3. `POST /v1/default/banks/{bank_id}/memories/recall` (read path — the
     big one; BM25 + vector + RRF + rerank logic).
  4. Entities, documents, mental models, directives, and anti-patterns.
  5. Audit, operations, files, and webhooks.
  6. MCP HTTP wire.
- For each endpoint:
  - Implement in Go.
  - CI compares Go responses on a fixture set and **fails on divergence**.
  - When parity is held over a soak window (1+ weeks of green CI), flip the per-endpoint route at the reverse proxy (Caddy / nginx / Traefik) to Go.
- The `recall` endpoint is the critical one: BM25 + vector + RRF + reranker semantic parity is the migration's hardest gate. Allocate a full 2 weeks for it alone. Use a held-out evaluation set (`eval-workspace/`) to score retrieval quality — *recall@k* on a labelled benchmark must meet the frozen baseline.

### Phase 3 — worker + admin migration (2–3 weeks)

- Port background tasks (`consolidation`, `reflect`) to Go using the current DB schema and contract fixtures as reference.
- Admin UI: build with **`pony` (declarative TUI markup, atop `ultraviolet`)** for the view layer, served over SSH via `wish`. Replaces whatever admin/HTML the Python server serves today.
  - `bubbles` components used where `pony` doesn't have equivalents yet.
  - `harmonica` for animations (loading spinners, transitions).
  - `x/mosaic` if any inline image rendering is wanted (memory previews, retrieval result graphs).
  - `x/editor` for inline memory editing.
  - `x/vcr` to record admin sessions for audit (especially for destructive ops — bank deletion, mass anti-pattern edits).
  - `glamour` for rendering memory content (which is markdown).
  - `huh` for interactive forms (memory creation, bulk operations).
- Other `x/*` packages adopted where useful — the rule is: if it fits the use case, use it; we're betting on the Charm ecosystem comprehensively, not piecemeal.

> **Pony adoption note.** `pony` is flagged experimental by Charm ("primarily AI-generated as an exploration of declarative TUI frameworks. Use at your own risk"). We're adopting it anyway as a deliberate foundation bet — the TUI surface here is admin-only and tolerates churn better than user-facing surfaces. The view layer is architected to be swappable: if `pony` proves unworkable, the data/state layer survives and the view falls back to plain `bubbletea`. This is the same posture as adopting `fantasy` early: pick the shape the future will need, accept some churn cost during the bet.

### Phase 4 — fantasy-based central agent host (variable, depends on sf SPEC §17)

- Once persistent agents are scoped in sf SPEC §17, host them here.
- Each central agent is a fantasy `Agent` instance with tool registration.
- Inbox model from SPEC §18 (or the swarm-chat from sf ADR-011) wires up.
- sf project instances reach central agents via this server's MCP.

### Phase 5 — archive old runtime (1 week)

- All endpoints serving from Go for ≥ 4 weeks of clean ops.
- Stop any previous non-Go serving artifact.
- Keep Python source in repo for one release cycle as historical reference.
- Remove or archive it in the following minor release.

## Compatibility guarantees

| Guarantee | How |
|---|---|
| Existing clients keep working | Wire contract frozen; OpenAPI spec is the source of truth. |
| Data is zero-touch | Same database contents and schema. Go uses Postgres 18 + `vchord`/`vchord_bm25` without rewriting existing memory content. |
| Embedding gateway is direct | Go calls inference-fabric's OpenAI-compatible `/v1/embeddings` and `/v1/rerank` endpoints directly with batched inputs; no LiteLLM dependency is added. |
| MCP wire protocol unchanged | Verified by replaying recorded MCP sessions against Go and frozen transcripts. |
| Recall quality preserved | `eval-workspace/` evaluation set scores meet the frozen baseline. |
| Rollback always possible | Reverse proxy can flip any endpoint back to the previous deployment artifact during the soak period. |

## Risks and mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| BM25 / vector parity drift against the frozen baseline | High | Held-out evaluation set; recall@k threshold enforced in CI before flipping traffic. |
| Embedding dimension/profile mismatch | Medium | Pin the selected dimensions per profile, test inference-fabric request/response shape, and fail startup if the returned dimension does not match the configured DB vector dimension. |
| `pgx` + vchord custom-type decoder edge cases | Medium | Build minimal reproduction repos early and verify the layered Postgres 18 VectorChord container in CI. |
| Reranker latency regression in the Go inference-fabric client | Medium | Bench under load before flipping and keep the request contract fixture-driven. |
| Worker / consolidation logic drift | Medium | Port worker only after the read/write endpoints have soaked; consolidation is offline so easier to verify. |
| Operational complexity during transition (two servers, reverse proxy, two CI lanes) | Medium | Time-box the transition: aim for ≤ 12 weeks total. Frequent flips, small batches. |
| `fantasy` API churn during migration | Low–Medium | fantasy is at 730 stars and pushed daily but pre-1.0; pin a version, plan for one upgrade midway. |
| Missing Go equivalents for existing helper libraries | Low | Audit existing deps in Phase 0; identify Go equivalents or keep the behavior small and explicit. |

## Open questions

1. **Repo layout** — `go/` subdir of `singularity-memory`, or new `singularity-memory-go` repo? Recommend subdir during transition (one place to coordinate); split later.
2. **Charm-server: library or sidecar?** — Use `charm-server` patterns (SSH-key identity, encryption) as ported library code, or run `charm-server` as a sidecar process? Lean library: simpler ops, single binary deploy.
3. **Embedding dimensions** — `vchord` should benchmark native 2560 first and
   use it if evals/latency pass. The exact value must be selected by Phase 1
   benchmarks.
4. **MCP-over-stdio vs MCP-over-HTTP**: serve both?
5. **Drop FastAPI's auto-generated docs UI**? Replace with a Bubble Tea / Wish admin UI per Charm pattern? Probably yes — reduces dependency.
6. **Where does the server run** in the federation topology — `mikki-bunker`, `aidev`, or a dedicated Tailnet node? Probably a dedicated node for HA.

## Dependencies on related ADRs (sf side)

These ADRs live in the [singularity-forge](https://github.com/singularity-ng/singularity-forge) repo:

- **[sf ADR-012 — Multi-instance federation](https://github.com/singularity-ng/singularity-forge/blob/main/docs/dev/ADR-012-multi-instance-federation.md)** — Singularity Memory is the load-bearing federation primitive (Surface 1).
- **[sf ADR-013 — Network and remote-execution](https://github.com/singularity-ng/singularity-forge/blob/main/docs/dev/ADR-013-network-and-remote-execution.md)** — names Tailnet/Headscale as the deployment substrate. Singularity Memory deploys onto that substrate.
- **[sf ADR-014 — Singularity Knowledge + Agent Platform](https://github.com/singularity-ng/singularity-forge/blob/main/docs/dev/ADR-014-singularity-knowledge-and-agent-platform.md)** — the strategic ADR; this `MIGRATION.md` is its implementation arm.
- **[sf ADR-016 — Charm AI stack adoption](https://github.com/singularity-ng/singularity-forge/blob/main/docs/dev/ADR-016-charm-ai-stack-adoption.md)** — frames why Singularity Memory goes Go while sf core stays TS (parallel build, no core migration).
- **[sf ADR-017 — Charm TUI client](https://github.com/singularity-ng/singularity-forge/blob/main/docs/dev/ADR-017-charm-tui-client.md)** — independent but landing in the same time window; same Charm-stack adoption strategy.

Also relevant:

- **[sf SPEC.md §16](https://github.com/singularity-ng/singularity-forge/blob/main/SPEC.md)** — Knowledge Layer (the contract Singularity Memory implements).
- **[sf SPEC.md §17–18](https://github.com/singularity-ng/singularity-forge/blob/main/SPEC.md)** — Persistent Agents and Inter-Agent Messaging (Phase 4 target).
- **[sf BUILD_PLAN.md](https://github.com/singularity-ng/singularity-forge/blob/main/BUILD_PLAN.md)** — Tier 1+ active follow-ups; the sf-side row pointing back here.

## Decision log

| Date | Decision | By |
|---|---|---|
| 2026-04-29 | Migration plan drafted; build foundation on `charm` (auth/identity patterns) + `fantasy` (agent runtime) from day one rather than retrofit later. | sf strategy session |
| 2026-04-29 | Runtime embeddings/reranking target inference-fabric directly through OpenAI-compatible `/v1/embeddings` and `/v1/rerank`; no LiteLLM dependency in the Go migration. Storage/retrieval target is Postgres 18 + `vchord`/`vchord_bm25`; `pg0`, Apache AGE, TimescaleDB, and `pgvectorscale` are not first-cut migration targets. | sf strategy session |

This doc is owned alongside `TODO.md` — TODO captures pre-migration production-readiness; this captures the migration itself. They should be read together.
