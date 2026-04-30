# Migration Debate Synthesis

Status: sf auto execution artifact
Date: 2026-04-29

## Outcome

Proceed with the Go migration under the current constraints. The plan is
concrete enough for sf auto to continue through Phase 0 and Phase 1 units, but
retain/recall and MCP must remain gated by fixtures and evals.

## Advocate Position

The Go runtime is already taking shape: config, PG18 VectorChord compose,
OpenAI-compatible embedding/rerank clients, health/version, OpenAPI serving,
and bank endpoints exist. Continuing in Go avoids spending effort on the old
runtime and gives sf a clearer execution target.

## Challenger Position

The risk is not the first bank endpoint. The risk is silent semantic drift in
retain/recall, MCP tool behavior, and retrieval ranking. The answer is not more
fallbacks; it is frozen fixtures, deterministic DB state, and recall evals
before flipping traffic.

## Architect Position

Keep the shape narrow:

- one database profile: Postgres 18 + VectorChord suite;
- one inference surface: `https://llm-gateway.centralcloud.com/v1`;
- one embedding default: Qwen3-Embedding-4B native 2560D;
- one runtime target: Go.

Do not add first-cut abstractions for `pg0`, LiteLLM, TimescaleDB, Apache AGE,
`pgvectorscale`, or legacy env aliases.

## Delivery Position

The next execution units should be:

1. Finish Phase 0 contract artifacts: HTTP fixtures and MCP transcripts.
2. Add Go contract fixture comparison tests.
3. Add PG18 VectorChord extension smoke in CI.
4. Freeze bank list/profile/create/update/delete parity fixtures.
5. Start retain write path only after embedding dimension/write tests exist.

## Operations Position

Cutover must be per endpoint group and reversible at the routing layer. The
previous serving artifact remains available until every endpoint has soaked on
Go and rollback has been drilled. No traffic flip should mutate existing memory
content as part of the cutover.

## Decision

Continue. If sf cannot produce deterministic fixtures or recall eval baselines,
it must enter reassess before implementing retain/recall instead of inventing
behavior from the old runtime.
