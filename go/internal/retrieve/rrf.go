package retrieve

import (
	"sort"
)

// ---------------------------------------------------------------------------
// Lane identifiers
// ---------------------------------------------------------------------------

// Lane represents a retrieval lane.
type Lane string

const (
	LaneSemantic  Lane = "semantic"
	LaneBM25      Lane = "bm25"
	LaneGraph     Lane = "graph"
	LaneTemporal  Lane = "temporal"
)

// ---------------------------------------------------------------------------
// RRF configuration
// ---------------------------------------------------------------------------

// RRFConfig holds parameters for Reciprocal Rank Fusion.
type RRFConfig struct {
	K       int
	Weights map[Lane]float64
}

// DefaultRRFConfig returns the standard configuration used by the recall
// endpoint (k=60, weights: semantic=1.0, bm25=1.0, graph=0.5, temporal=0.3).
func DefaultRRFConfig() RRFConfig {
	return RRFConfig{
		K: 60,
		Weights: map[Lane]float64{
			LaneSemantic: 1.0,
			LaneBM25:     1.0,
			LaneGraph:    0.5,
			LaneTemporal: 0.3,
		},
	}
}

// ---------------------------------------------------------------------------
// Retrieval result (input to RRF)
// ---------------------------------------------------------------------------

// RetrievalResult is a single result from any retrieval lane.
type RetrievalResult struct {
	ID    string
	Score float64
}

// RRFInput bundles results from one lane.
type RRFInput struct {
	Lane    Lane
	Results []RetrievalResult
}

// ---------------------------------------------------------------------------
// Merged candidate (output of RRF)
// ---------------------------------------------------------------------------

// MergedCandidate is the fused output for a unique document.
type MergedCandidate struct {
	ID          string
	RRFScore    float64
	RRFRank     int
	SourceRanks map[Lane]int // lane -> original rank
}

// ---------------------------------------------------------------------------
// Reciprocal Rank Fusion
// ---------------------------------------------------------------------------

// ReciprocalRankFusion merges multiple ranked result lists using RRF.
//
// For each lane, results are sorted by Score descending and assigned ranks
// 1..N.  For every unique ID the weighted RRF score is:
//
//	score(d) = sum_over_lanes( weight[lane] * 1 / (k + rank(d)) )
//
// The final list is sorted by RRF score descending.  When two candidates
// have the same RRF score, the one with the larger sum of raw scores
// (across all lanes) wins.
func ReciprocalRankFusion(inputs []RRFInput, config RRFConfig) []MergedCandidate {
	k := config.K
	if k <= 0 {
		k = 60
	}

	// Accumulators keyed by document ID.
	rrfScores := make(map[string]float64)
	rawScoreSum := make(map[string]float64)
	sourceRanks := make(map[string]map[Lane]int)

	for _, in := range inputs {
		weight := config.Weights[in.Lane]
		if weight == 0 {
			weight = 1.0
		}

		// Sort descending by raw score so rank 1 = highest score.
		results := make([]RetrievalResult, len(in.Results))
		copy(results, in.Results)
		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})

		for rank, r := range results {
			// rank is 0-based here; formula uses 1-based rank.
			oneBasedRank := rank + 1
			docID := r.ID

			rrfScores[docID] += weight * (1.0 / (float64(k) + float64(oneBasedRank)))
			rawScoreSum[docID] += r.Score

			if sourceRanks[docID] == nil {
				sourceRanks[docID] = make(map[Lane]int)
			}
			sourceRanks[docID][in.Lane] = oneBasedRank
		}
	}

	// Build merged candidates.
	merged := make([]MergedCandidate, 0, len(rrfScores))
	for docID, score := range rrfScores {
		merged = append(merged, MergedCandidate{
			ID:          docID,
			RRFScore:    score,
			SourceRanks: sourceRanks[docID],
		})
	}

	// Sort by RRF score descending; tie-break by raw score sum descending.
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].RRFScore != merged[j].RRFScore {
			return merged[i].RRFScore > merged[j].RRFScore
		}
		return rawScoreSum[merged[i].ID] > rawScoreSum[merged[j].ID]
	})

	// Assign final RRF ranks (1-based).
	for i := range merged {
		merged[i].RRFRank = i + 1
	}

	return merged
}

// ---------------------------------------------------------------------------
// Budget mapping
// ---------------------------------------------------------------------------

// BudgetToLimit maps a budget level to an over-fetch limit for the recall
// pipeline.  The handler may trim the final result set to a smaller number.
func BudgetToLimit(budget string) int {
	switch budget {
	case "low":
		return 5
	case "mid":
		return 10
	case "high":
		return 20
	default:
		return 10 // default to mid
	}
}
