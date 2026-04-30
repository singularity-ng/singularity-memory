# Memory Recall Fixture Format

This document defines the JSON fixture format used for static, fixture-based RRF
parity verification between the Python reference implementation
(`src/singularity_memory_server/engine/search/fusion.py`) and the Go port
(`go/internal/retrieve/rrf.go`).

## Purpose

Fixtures encode per-lane ranked retrieval results so that the Reciprocal Rank
Fusion (RRF) layer can be tested in isolation without a live database or
embedding service.  Each fixture is a self-contained input/output pair: given a
query and a set of lane results, the expected fused ranking is known in advance.

## File Layout

```
eval-workspace/memory-recall/fixtures/
├── FORMAT.md          (this file)
├── 001-simple-overlap.json
├── 002-weighted-dominance.json
├── 003-tie-break.json
└── ... (additional scenarios)
```

## Schema

A fixture is a single JSON object with the following top-level fields:

| Field            | Type   | Required | Description |
|------------------|--------|----------|-------------|
| `name`           | string | yes      | Human-readable scenario name. |
| `description`    | string | yes      | What the fixture exercises (e.g. "two-lane overlap with equal weights"). |
| `query`          | string | yes      | The original recall query string. |
| `k`              | int    | no       | RRF constant `k`. Defaults to `60` when omitted. |
| `weights`        | object | no       | Per-lane weight overrides. Keys are lane names (`semantic`, `bm25`, `graph`, `temporal`). Defaults match `DefaultRRFConfig` (semantic=1.0, bm25=1.0, graph=0.5, temporal=0.3). |
| `lanes`          | object | yes      | Map of lane name → ordered list of results for that lane. |
| `expected_top_k` | array  | yes      | Ordered list of `{id, rank}` objects representing the correct fused ranking. |

### `lanes` object

Each key is a lane name.  Supported lanes:
- `semantic`
- `bm25`
- `graph`
- `temporal`

The value for each lane is an array of **Result** objects:

```json
{
  "id": "mem-001",
  "score": 0.95
}
```

| Field   | Type   | Description |
|---------|--------|-------------|
| `id`    | string | Unique document / memory identifier. |
| `score` | number | Raw retrieval score from the lane.  Used only for intra-lane sorting (descending) and tie-breaking; it does **not** directly affect the RRF score. |

The array order in the fixture is the *expected* rank order for that lane
(1 = highest score).  When the harness loads the fixture it must sort each
lane by `score` descending before feeding it to RRF, matching the behaviour
of both Python and Go implementations.

### `expected_top_k` array

Each element:

```json
{
  "id": "mem-001",
  "rank": 1
}
```

| Field  | Type   | Description |
|--------|--------|-------------|
| `id`   | string | Document identifier. |
| `rank` | int    | 1-based position after RRF fusion. |

The array must be ordered by ascending `rank`.

## RRF Computation Rules (Reference)

Both implementations must agree on the following rules so that fixtures are
portable:

1. **Intra-lane sorting** – Results within a lane are sorted by `score`
   descending.  Rank 1 is assigned to the highest score.
2. **Intra-lane deduplication** – If the same `id` appears more than once in a
   lane, only the first (highest-score) occurrence counts; subsequent
   occurrences are ignored.
3. **RRF score** – For each unique document `d`:
   ```
   score(d) = Σ_lane  weight[lane] * (1 / (k + rank_lane(d)))
   ```
   where `rank_lane(d)` is the 1-based rank of `d` in that lane, or omitted
   if `d` does not appear in the lane.
4. **Tie-breaking** – When two documents have the same RRF score, the one with
   the larger sum of raw scores across all lanes wins.
5. **Final ranking** – Documents are sorted by RRF score descending, then by
   raw-score sum descending.  Ranks are assigned 1..N in that order.

## Example Fixtures

### 001-simple-overlap.json

Two lanes (`semantic` and `bm25`) with one overlapping document.  Equal
weights, `k=60`.

```json
{
  "name": "simple-overlap",
  "description": "Two-lane overlap with equal weights. Document 'b' appears in both lanes and should win.",
  "query": "machine learning frameworks",
  "k": 60,
  "weights": {
    "semantic": 1.0,
    "bm25": 1.0
  },
  "lanes": {
    "semantic": [
      { "id": "a", "score": 0.90 },
      { "id": "b", "score": 0.80 }
    ],
    "bm25": [
      { "id": "b", "score": 0.95 },
      { "id": "c", "score": 0.70 }
    ]
  },
  "expected_top_k": [
    { "id": "b", "rank": 1 },
    { "id": "a", "rank": 2 },
    { "id": "c", "rank": 3 }
  ]
}
```

**Why `b` wins:**
- `b` ranks: semantic=2, bm25=1 → RRF = 1/62 + 1/61 ≈ 0.03252
- `a` ranks: semantic=1 → RRF = 1/61 ≈ 0.01639
- `c` ranks: bm25=2 → RRF = 1/62 ≈ 0.01613

### 002-weighted-dominance.json

All four lanes return the same single document.  Default weights are used so
that the contribution of each lane is known precisely.

```json
{
  "name": "weighted-dominance",
  "description": "Same document in all four lanes with default weights. Verifies that each lane contributes the correct weighted score.",
  "query": "distributed systems",
  "lanes": {
    "semantic": [
      { "id": "x", "score": 0.5 }
    ],
    "bm25": [
      { "id": "x", "score": 0.5 }
    ],
    "graph": [
      { "id": "x", "score": 0.5 }
    ],
    "temporal": [
      { "id": "x", "score": 0.5 }
    ]
  },
  "expected_top_k": [
    { "id": "x", "rank": 1 }
  ]
}
```

**Expected score:**
- RRF = (1.0 + 1.0 + 0.5 + 0.3) / 61 ≈ 0.04590

### 003-tie-break.json

A scenario where raw RRF scores are identical but tie-breaking by raw-score
sum changes the order.

```json
{
  "name": "tie-break",
  "description": "Identical RRF scores for two documents; tie-breaker resolves order based on raw score sum. A third lane gives 'a' a tiny edge.",
  "query": "neural network architecture",
  "k": 60,
  "weights": {
    "semantic": 1.0,
    "bm25": 1.0,
    "graph": 1.0
  },
  "lanes": {
    "semantic": [
      { "id": "a", "score": 10.0 },
      { "id": "b", "score": 5.0 }
    ],
    "bm25": [
      { "id": "b", "score": 10.0 },
      { "id": "a", "score": 5.0 }
    ],
    "graph": [
      { "id": "a", "score": 0.1 }
    ]
  },
  "expected_top_k": [
    { "id": "a", "rank": 1 },
    { "id": "b", "rank": 2 }
  ]
}
```

**Why `a` wins:**
- Without the graph lane both `a` and `b` have RRF = 1/61 + 1/62 and raw sum = 15.
- With the graph lane `a` gets an extra 0.1/61 ≈ 0.00164 and a higher raw sum
  (15.1 vs 15), so it wins on both score and tie-break.

## Loading Fixtures in Go

A minimal loader type (suitable for a test harness) is:

```go
type Fixture struct {
    Name          string                       `json:"name"`
    Description   string                       `json:"description"`
    Query         string                       `json:"query"`
    K             int                          `json:"k"`
    Weights       map[string]float64           `json:"weights"`
    Lanes         map[string][]FixtureResult   `json:"lanes"`
    ExpectedTopK  []FixtureExpected            `json:"expected_top_k"`
}

type FixtureResult struct {
    ID    string  `json:"id"`
    Score float64 `json:"score"`
}

type FixtureExpected struct {
    ID   string `json:"id"`
    Rank int    `json:"rank"`
}
```

## Loading Fixtures in Python

A minimal loader:

```python
import json
from pathlib import Path
from dataclasses import dataclass
from typing import List, Dict

@dataclass
class Fixture:
    name: str
    description: str
    query: str
    k: int
    weights: Dict[str, float]
    lanes: Dict[str, List[dict]]
    expected_top_k: List[dict]

def load_fixture(path: Path) -> Fixture:
    data = json.loads(path.read_text())
    return Fixture(
        name=data["name"],
        description=data["description"],
        query=data["query"],
        k=data.get("k", 60),
        weights=data.get("weights", {}),
        lanes=data["lanes"],
        expected_top_k=data["expected_top_k"],
    )
```

## Extending the Fixture Suite

When adding new fixtures:

1. Choose a descriptive file name (`NNN-<kebab-case>.json`).
2. Set `description` to explain the scenario and the expected behaviour.
3. Ensure `expected_top_k` is derived from the Python reference implementation
   (or manually verified against the RRF formula).
4. Keep fixture files small and focused — one conceptual scenario per file.
5. Document any non-default `k` or `weights` values in the fixture description.
