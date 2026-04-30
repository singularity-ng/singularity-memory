# DB And Retrieval Research

## Scope

This lane documents the database and retrieval target for the Python-to-Go
migration. It is grounded in `MIGRATION.md`, current Docker/CI wiring, the
Python reference retrieval engine, and the existing Go scaffold.

The production first-cut target is external Postgres 18 plus the VectorChord
suite: `vector`, `vchord`, `pg_tokenizer`, `vchord_bm25`, and `pg_trgm`.
Retrieval semantics stay BM25 + vector + RRF + rerank. The migration is not a
retrieval redesign and is not a database swap.

## Repo Evidence

- `MIGRATION.md` defines the storage target as external Postgres 18 with
  VectorChord suite and says embedded `pg0` is not a Go migration target.
- `docker/postgres-vchord/Dockerfile` layers on
  `ghcr.io/tensorchord/vchord-suite:pg18-latest`.
- `docker/postgres-vchord/initdb/001-vectorchord.sql` enables `vector`,
  `vchord`, `pg_tokenizer`, `vchord_bm25`, and `pg_trgm`.
- `docker-compose.yaml` builds `singularity-memory-postgres-vchord:pg18`,
  sets `SINGULARITY_STORAGE_PROFILE=vchord`,
  `SINGULARITY_VECTOR_EXTENSION=vchord`, and
  `SINGULARITY_TEXT_SEARCH_EXTENSION=vchord`.
- `README.md` names production vector as `vchord` / `vchordrq`, production
  lexical as `vchord_bm25`, and explicitly excludes `pg0`, Apache AGE, and
  TimescaleDB from the first Go migration.
- `.github/workflows/ci.yml` uses the same `vchord-suite:pg18-latest` service
  and `SINGULARITY_STORAGE_PROFILE=vchord`.
- `go/internal/config/config.go` defaults the Go storage profile to `vchord`.
- `src/singularity_memory_server/engine/search/retrieval.py` is the reference
  for combined semantic + BM25 retrieval, temporal retrieval, and multi-fact
  parallel retrieval.
- `src/singularity_memory_server/engine/search/fusion.py` is the RRF reference.
- `src/singularity_memory_server/engine/search/link_expansion_retrieval.py`
  shows the current graph lane is relational link expansion over
  `memory_links` and entity joins, not Apache AGE/openCypher.
- Alembic migrations `d5e6f7a8b9c0...` and `a4b5c6d7e8f9...` create and repair
  per-bank, per-fact-type vector indexes using `vchordrq` when
  `SINGULARITY_VECTOR_EXTENSION=vchord`.
- `src/singularity_memory_server/migrations.py` already treats vector
  dimension mismatch as a hard startup/data issue when rows with embeddings
  exist.

## Production Target

Use one production storage profile for the first Go cut:

```text
Postgres: Postgres 18
Extension image: ghcr.io/tensorchord/vchord-suite:pg18-latest
Vector type: pgvector-compatible vector
Vector index: vchordrq
Lexical index: vchord_bm25
Tokenizer support: pg_tokenizer
Similarity profile: native Qwen3-Embedding-4B 2560D first
```

Required extensions at database bootstrap:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS vchord CASCADE;
CREATE EXTENSION IF NOT EXISTS pg_tokenizer CASCADE;
CREATE EXTENSION IF NOT EXISTS vchord_bm25 CASCADE;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

This should be the only first-cut Go migration target. Do not add first-cut
branches for embedded `pg0`, `pgvectorscale`, Apache AGE, or TimescaleDB.
Legacy compatibility that exists in the Python reference can remain there as
history, but the Go migration should not preserve it as deployable production
surface.

## Retrieval Contract To Preserve

The recall path must preserve the reference shape:

- Dense semantic retrieval against `memory_units.embedding` using
  `ORDER BY embedding <=> $query::vector`.
- Lexical retrieval through `vchord_bm25` when
  `SINGULARITY_TEXT_SEARCH_EXTENSION=vchord`, using `search_vector <&>
  to_bm25query(...)` and `tokenize(..., 'llmlingua2')`.
- Per-fact-type retrieval for `world`, `experience`, and `observation`.
- Over-fetching of approximate vector results before trimming to the requested
  limit.
- Temporal retrieval as its own lane.
- Graph retrieval as relational link expansion seeded by semantic/temporal
  results, using entity, semantic, and causal links from relational tables.
- RRF fusion over semantic, BM25, graph, and temporal lanes.
- Optional reranking after fusion through the gateway documented in
  `embedding-gateway.md`.

Do not replace the lane model with a new ranking design during migration. Any
desired redesign belongs behind a separate ADR after the Go parity gate passes.

## Indexing Notes

The migration should keep the per-bank, per-fact-type vector-index strategy.
The reference migrations explain why fact-type-only partial indexes lose to the
bank B-tree predicate and why the global vector index competes with larger
partitions. The Go port should assume that banks need indexes shaped like:

```sql
CREATE INDEX IF NOT EXISTS idx_mu_emb_<fact>_<bank_internal_id>
ON memory_units USING vchordrq (embedding vector_l2_ops)
WHERE fact_type = '<fact_type>' AND bank_id = '<bank_id>';
```

The existing `a4b5c6d7e8f9...` migration is important evidence: historical
Python compatibility accidentally left some HNSW indexes in place, then repaired
them to `vchordrq` for `vchord`. The Go target should avoid carrying that branch
logic forward as new runtime policy.

Current Go status: `go/internal/store` carries the parsed storage profile and
generates `vchordrq (embedding vector_l2_ops)` partial indexes for the default
`vchord` profile when a bank is created. `pgvector` still generates HNSW as a
compatibility profile, but it is not the production migration target.

For text search, the first-cut target is `search_vector
bm25_catalog.bm25vector` plus a BM25 index using
`bm25_catalog.bm25_ops`. Native PostgreSQL `tsvector` and Timescale
`pg_textsearch` behavior are reference-history/fallback behavior, not the Go
production target.

## 2560D Implications

Qwen3-Embedding-4B is native 2560-dimensional. The first benchmark/profile
should be native 2560D with `vchordrq`. This is why the production target is not
plain pgvector HNSW: the Python migration helper already guards pgvector HNSW
above 2000 dimensions, while the migration plan calls out `vchord` as the
high-dimensional target.

Operational rules:

- Store the selected dimension as an explicit profile expectation.
- Fail startup if the gateway returns a vector dimension that differs from the
  database column type when embeddings already exist.
- Do not silently truncate vectors unless
  `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` is explicitly set for a measured
  experiment.
- Benchmark 1024, 1536, and 2000 only as fallback experiments. Native 2560D is
  the production preference unless evals or latency prove otherwise.

## Exclusions

These are intentionally not first-cut Go migration targets:

- `pg0`: agents already need networked inference, and local/dev has the
  Postgres 18 VectorChord container.
- `pgvectorscale`: Python compatibility may remain, but the Go plan should not
  add DiskANN branches as migration gates.
- Apache AGE: current graph retrieval is relational link expansion, not
  openCypher.
- TimescaleDB / `pg_textsearch`: potentially useful later for operations or
  metrics, but not required for retain/recall parity.
- Legacy env compatibility for alternate storage profiles: the Go production
  docs and first-cut implementation should use the explicit VectorChord target
  names only.

## Verification Gates

Minimum verification for this lane:

- Compose/CI boots Postgres 18 VectorChord and verifies all required extensions
  are installed.
- Go config accepts `SINGULARITY_STORAGE_PROFILE=vchord` and rejects or ignores
  unsupported first-cut profile names.
- Retain writes 2560D vectors into `memory_units.embedding` when the gateway is
  configured for native Qwen3-Embedding-4B.
- Recall parity fixtures cover semantic, BM25, graph, temporal, RRF, and rerank
  output shape.
- Held-out recall@k evals meet the frozen Python baseline before any production
  route flip.
