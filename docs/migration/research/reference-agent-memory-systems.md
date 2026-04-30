# Reference Agent-Memory Systems

**Date:** 2026-04-29
**Scope:** server-side migration research for Singularity Memory.

This research lane compares Singularity Memory with nearby systems and records
what to borrow for the Go/Charm server migration. Hermes/OpenClaw/MCP clients
are compatibility surfaces only; the migration design target is the server.

## Current Singularity Memory Boundary

Singularity Memory is a shared memory server:

- HTTP API and MCP-compatible surface.
- Postgres-backed storage targeting Postgres 18 with `vector`, `vchord`,
  `pg_tokenizer`, `vchord_bm25`, and `pg_trgm`.
- Retain, recall, reflect, banks, entities, mental models, directives,
  operations, files, webhooks, audit logs, and workers.
- Retrieval pipeline: lexical search + vector search + RRF fusion + optional
  reranking.
- Thin client adapters are secondary; they must remain thin HTTP clients.

The Go migration should preserve that boundary. It should not become "the
Hermes adapter" and it should not become a full agent framework before the
server contract is stable.

## Reference Systems

| System | What it is | What Singularity Memory should borrow | What not to copy |
|---|---|---|---|
| Agent-brain systems | Agent-readable knowledge stores with pages, graph/search, durable jobs, subagent runs, and operational checks. | Brain pages, sources, links/backlinks, timeline entries, durable background jobs, health/doctor checks, reachability evals, fail-improve loops, agent-readable operational output, graph-backed search ergonomics. | Do not move a large skills product into the core server. Keep skills/adapters outside the server boundary. |
| [Cavemem](https://github.com/JuliusBrussee/cavemem) | Local cross-agent memory for coding assistants: hooks write observations, SQLite stores compressed text plus FTS/vector indexes, MCP exposes progressive retrieval. | Progressive disclosure (`search`/timeline returns compact hits, full bodies fetched explicitly), redaction at the write boundary, deterministic compression that preserves technical tokens, status/doctor UX, and non-blocking background embedding/backfill ergonomics. | Do not replace the shared server with local SQLite, do not make hooks/adapters the core product, and do not put lossy compression in the authoritative memory text path before recall parity is measured. |
| [Letta / MemGPT](https://docs.letta.com/guides/agents/architectures/memgpt) | Stateful agent framework using core, recall, and archival memory with tool-managed memory movement. | Explicit memory classes: always-visible profile/core facts, searchable episodic recall, long-term archival memory, and agent-managed writes with auditability. | Do not require Singularity Memory to own the whole conversation runtime in Phases 0-3. |
| [Zep / Graphiti](https://www.getzep.com/product/knowledge-graph-mcp/) and [getzep/graphiti](https://github.com/getzep/graphiti) | Temporal knowledge graph memory for agents and MCP clients. | Temporal facts, relationship history, entity/fact versioning, contradiction/supersession semantics, graph query as a first-class recall lane. | Do not switch the primary DB away from Postgres during this migration. Apache AGE or sidecar graph stays optional until parity gates pass. |
| [Mem0 / OpenMemory](https://mem0.ai/openmemory) | Universal memory layer and MCP memory server with project-scoped auto-capture and audit/control UI. | Project/repo scoping, memory visibility controls, access logs, edit/delete UX, simple MCP memory mental model. | Do not add managed-service assumptions or vendor-specific runtime coupling. |
| [LangMem / LangGraph long-term memory](https://docs.langchain.com/oss/javascript/langchain/long-term-memory) | LangGraph/LangChain memory tools and stores, with semantic/episodic/procedural memory patterns. | The semantic/episodic/procedural taxonomy, background memory extraction, and procedural memory as tested operating instructions rather than loose text. | Do not couple the server to LangChain/LangGraph. |
| [Cognee](https://docs.cognee.ai/examples/overview) | Memory/knowledge engine combining graph traversal and vector retrieval over broad data ingestion. | Data-ingestion pipeline ideas, graph/vector hybrid query modes, ontology/domain model hooks, visualization/admin ideas. | Do not expand ingestion scope before HTTP/retrieval parity is green. |

## Server Anatomy

Use this as the migration's architectural decomposition. The names are
mnemonic; the implementation should use normal package names.

| Layer | Server responsibility | Borrowed pattern |
|---|---|---|
| Head | Memory model, planning for recall, source-of-truth schemas, frozen API contract. | Letta memory hierarchy; LangMem memory taxonomy. |
| Heart | Retain/recall/reflect engine, retrieval quality, consolidation, contradiction handling. | Graphiti temporal relationships; Cognee graph/vector hybrid retrieval. |
| Hands | Server tools and command surfaces: HTTP routes, MCP tools, admin commands. | Mem0/OpenMemory simple memory operations and audit UX. |
| Feet | Durable background execution: retain batches, backfill, consolidation, evaluation jobs. | Durable jobs: timeouts, retries, stall recovery, transcripts. |
| Nerves | Observability, health, traces, evals, audit, doctor/repair. | Doctor checks, reachability checks, audit/access logs. |

## Migration Implications

1. Phase 0 must freeze the HTTP/OpenAPI contract and add integration tests
   before more Go endpoint work.
2. Phase 1 must keep the Go server small but real: config, pgx store,
   embedding/rerank clients, storage profiles, `/healthz`, `/version`, and
   first parity endpoints.
3. Phase 2 must port retrieval without redesigning it. Add graph/temporal
   improvements only after Python/Go recall parity is measurable.
4. The Go memory service now has a concrete native brain layer:
   `brain_pages`, `brain_sources`, `brain_links`, `brain_timeline_entries`,
   raw/version tables, and `brain_jobs`, plus HTTP handlers and an
   importer. Pages are also written as memory units, so recall can retrieve
   them through the normal BM25/vector/RRF lanes.
5. Phase 3 should treat background work as a server subsystem, not an agent
   side effect. The durable job model needs job state, timeout, stalled
   detection, child completion events, retry/backoff, and structured logs.
6. MCP and agent-facing reads should use Cavemem-style progressive disclosure:
   compact ranked hits first, then explicit fetches for full memories, chunks,
   entities, and source facts. This keeps context use predictable while the
   authoritative text remains in Postgres.
7. Phase 4 can host persistent agents with `fantasy` only after the server
   has stable memory, jobs, evals, and operational controls.

## Design Decisions

- **Server first.** Adapters remain thin and are tested as downstream clients.
- **Postgres first.** Graph and vector improvements must fit the Postgres 18 +
  VectorChord target. Embedded DB profiles and `pg0` are not part of the first
  Go runtime.
- **Memory taxonomy is part of the server contract.** New endpoints and DB
  fields should distinguish semantic facts, episodic events, procedural
  instructions, and agent/runtime state instead of storing everything as an
  undifferentiated blob.
- **Durable jobs are not optional polish.** Retain/backfill/consolidation
  need server-owned execution state before production cutover.
- **Self-improvement is gated.** Fail-improve loops may propose deterministic
  rule/test changes, but server behavior changes must land through normal code
  review, tests, and migration gates.

## Follow-Up Research Questions

- Which current Python tables already map cleanly to semantic, episodic, and
  procedural memory?
- Should temporal relationships live in existing entity/fact tables first, or
  behind an optional graph lane?
- What is the minimum durable job schema that supports retain batch splitting,
  backfill, consolidation, and future central-agent work?
- Which recall eval dataset should become the stable benchmark for parity and
  future graph improvements?
- Which admin/doctor commands are required before flipping any Go-served route
  into production?
