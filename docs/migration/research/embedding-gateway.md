# Embedding Gateway Research

## Scope

This lane documents the embedding and reranking gateway target for the Go
migration. The target is the llm-gateway OpenAI-compatible API at
`https://llm-gateway.centralcloud.com/v1`, using Qwen3 embeddings and reranking
directly. Do not introduce LiteLLM or legacy provider compatibility into the
first-cut Go runtime.

## Repo Evidence

- `MIGRATION.md` requires direct inference-fabric / llm-gateway calls to
  `POST /v1/embeddings` and `POST /v1/rerank`, with no LiteLLM dependency.
- `MIGRATION.md` says Qwen3-Embedding-4B is native 2560D and should be
  benchmarked/preferred at native size first for the `vchordrq` profile.
- `docker-compose.yaml` defaults
  `SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL` and
  `SINGULARITY_RERANK_OPENAI_BASE_URL` to
  `https://llm-gateway.centralcloud.com/v1`.
- `docker-compose.yaml` defaults the embedding model to
  `qwen/qwen3-embedding-4b` and the rerank model to
  `qwen/qwen3-reranker-4b`.
- `README.md` documents the same base URL and says
  `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` should be unset for model-native
  output.
- `go/internal/config/config.go` defaults the Go embedding model to
  `qwen/qwen3-embedding-4b`, uses `0` dimensions to omit the request field, and
  defaults embedding batch size to 32.
- `go/internal/embed/embed.go` implements an OpenAI-compatible batched
  embeddings client, sorts provider responses by `index`, preserves input
  order, omits `dimensions` when unset, and fails vector-count mismatch.
- `go/internal/embed/embed_test.go` covers `/v1/embeddings` endpoint generation
  from both root and `/v1` base URLs, dimensions omission, explicit dimensions,
  response reordering, batch splitting, and vector-count mismatch.
- `go/internal/rerank/rerank.go` implements an OpenAI-compatible
  `/v1/rerank` client using `model`, `query`, `documents`, and `top_n`.
- `go/internal/rerank/rerank_test.go` covers `/v1/rerank` endpoint generation
  from both root and `/v1` base URLs and response order handling.
- The Python reference still contains many provider branches
  (`local`, TEI, Cohere, Gemini, OpenRouter, LiteLLM). Those are reference
  history, not first-cut Go runtime scope.

## Production Target

Use the llm-gateway OpenAI-compatible surface directly:

```text
Embeddings base URL: https://llm-gateway.centralcloud.com/v1
Embeddings endpoint: POST /v1/embeddings
Embeddings model: qwen/qwen3-embedding-4b
Native dimensions: 2560
Dimensions request field: omitted for production native output

Rerank base URL: https://llm-gateway.centralcloud.com/v1
Rerank endpoint: POST /v1/rerank
Rerank model: qwen/qwen3-reranker-4b
```

Required first-cut runtime env shape:

```text
SINGULARITY_EMBEDDINGS_PROVIDER=openai
SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1
SINGULARITY_EMBEDDINGS_OPENAI_MODEL=qwen/qwen3-embedding-4b
SINGULARITY_EMBEDDINGS_OPENAI_API_KEY=<gateway key if required>
SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS=

SINGULARITY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1
SINGULARITY_RERANK_MODEL=qwen/qwen3-reranker-4b
```

`SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` must remain unset or empty for native
2560D Qwen3-Embedding-4B output. Set it only for explicit benchmark runs of
1024, 1536, or 2000-dimensional truncation.

## Embeddings Contract

Request shape:

```json
{
  "model": "qwen/qwen3-embedding-4b",
  "input": ["text 1", "text 2"]
}
```

Optional dimensions experiment shape:

```json
{
  "model": "qwen/qwen3-embedding-4b",
  "input": ["text 1", "text 2"],
  "dimensions": 1536
}
```

Response shape expected by the Go client:

```json
{
  "data": [
    {"index": 0, "embedding": [0.1, 0.2]},
    {"index": 1, "embedding": [0.3, 0.4]}
  ],
  "model": "qwen/qwen3-embedding-4b"
}
```

Client requirements:

- Always send batched `input` arrays, including for one text.
- Preserve the order of caller inputs in returned vectors.
- Sort response data by `index` when a provider returns rows out of order.
- Split oversized provider requests by configured batch size, then append
  batches in caller order.
- Fail when response vector count does not match input count.
- Omit `dimensions` unless a positive explicit value is configured.
- Accept base URLs configured as either gateway root or `/v1` base without
  producing `/v1/v1/...`.
- Treat non-2xx gateway responses as hard request failures with model context
  in the error.

## Retain Batching Contract

The Go retain path must preserve the Python engine's two-level batching:

- Large retain requests are first split by retain/chunk/token policy.
- Each retain sub-batch sends all texts for that sub-batch to the embedding
  client as one ordered list.
- The embedding client may split by provider batch size internally.
- Every provider request still uses an array `input`.
- The final returned vector list must exactly align with the source fact list.

This matters because a silent vector-count mismatch or order drift corrupts
memory rows. The existing Go tests already cover the gateway-client side; retain
integration tests must cover the full DB write side.

## Rerank Contract

Request shape:

```json
{
  "model": "qwen/qwen3-reranker-4b",
  "query": "user query",
  "documents": ["candidate 1", "candidate 2"],
  "top_n": 10
}
```

Response shape expected by the Go client:

```json
{
  "results": [
    {"index": 1, "relevance_score": 0.95},
    {"index": 0, "relevance_score": 0.85}
  ]
}
```

Rerank is applied after RRF fusion. The Go port must preserve the candidate
construction and scoring semantics from the Python reference, then use the
gateway only for the cross-encoder score. Endpoint transport changes must not
change recall semantics.

## Dimension Policy

Production default is native Qwen3-Embedding-4B 2560D. The Go config convention
is:

- `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` unset/empty -> omit
  `dimensions`, use model-native 2560D.
- Positive integer -> send `dimensions` for a measured truncation experiment.
- Zero in Go config -> same as unset.

The DB profile must match the returned vector dimension. When existing rows have
embeddings, dimension mismatch is a hard failure requiring re-embedding or a
matching model/profile, not an implicit schema rewrite.

## Exclusions

Do not include these in the first-cut Go gateway target:

- LiteLLM proxy or LiteLLM SDK paths.
- TEI/local SentenceTransformers runtime paths.
- Cohere/Gemini/OpenRouter embedding provider branches.
- Shared legacy API-key fallback behavior from the Python config.
- Legacy env compatibility for alternate provider names or non-gateway base
  URL conventions.
- Automatic down-projection/truncation without an explicit dimensions setting.

The only provider surface for first-cut production is the explicit
OpenAI-compatible llm-gateway env set listed above.

## Verification Gates

Minimum verification for this lane:

- Unit tests prove `/v1/embeddings` and `/v1/rerank` endpoint construction from
  root and `/v1` base URLs.
- Unit tests prove batched embeddings preserve input order across provider
  response reordering and internal batch splitting.
- Unit tests prove `dimensions` is omitted when unset and included only when
  configured.
- Unit tests prove vector-count mismatch fails.
- Integration smoke test retains and recalls at least one memory through the
  gateway path.
- Startup or first write verifies returned vector dimension matches the
  selected DB profile before production traffic is flipped.

