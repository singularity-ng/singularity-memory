package retrieve

import (
	"math"
	"testing"
)

func TestReciprocalRankFusion_equalWeight(t *testing.T) {
	// Two lanes, one overlapping document.
	semantic := RRFInput{
		Lane: LaneSemantic,
		Results: []RetrievalResult{
			{ID: "a", Score: 0.9},
			{ID: "b", Score: 0.8},
		},
	}
	bm25 := RRFInput{
		Lane: LaneBM25,
		Results: []RetrievalResult{
			{ID: "b", Score: 0.95},
			{ID: "c", Score: 0.7},
		},
	}

	cfg := RRFConfig{K: 60, Weights: map[Lane]float64{
		LaneSemantic: 1.0,
		LaneBM25:     1.0,
	}}

	merged := ReciprocalRankFusion([]RRFInput{semantic, bm25}, cfg)

	if len(merged) != 3 {
		t.Fatalf("expected 3 merged candidates, got %d", len(merged))
	}

	// b appears in both lanes → highest score.
	if merged[0].ID != "b" {
		t.Errorf("expected top candidate 'b', got %s", merged[0].ID)
	}

	// Verify source ranks for b.
	if merged[0].SourceRanks[LaneSemantic] != 2 {
		t.Errorf("expected semantic rank 2 for b, got %d", merged[0].SourceRanks[LaneSemantic])
	}
	if merged[0].SourceRanks[LaneBM25] != 1 {
		t.Errorf("expected bm25 rank 1 for b, got %d", merged[0].SourceRanks[LaneBM25])
	}
}

func TestReciprocalRankFusion_weighted(t *testing.T) {
	// Same document in all four lanes; highest-weight lane should dominate
	// the score (all ranks are 1, so contribution = weight * 1/61).
	inputs := []RRFInput{
		{Lane: LaneSemantic, Results: []RetrievalResult{{ID: "x", Score: 0.5}}},
		{Lane: LaneBM25, Results: []RetrievalResult{{ID: "x", Score: 0.5}}},
		{Lane: LaneGraph, Results: []RetrievalResult{{ID: "x", Score: 0.5}}},
		{Lane: LaneTemporal, Results: []RetrievalResult{{ID: "x", Score: 0.5}}},
	}

	cfg := DefaultRRFConfig()
	merged := ReciprocalRankFusion(inputs, cfg)

	if len(merged) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(merged))
	}

	expected := (1.0 + 1.0 + 0.5 + 0.3) / 61.0
	if math.Abs(merged[0].RRFScore-expected) > 1e-9 {
		t.Errorf("expected RRF score %.9f, got %.9f", expected, merged[0].RRFScore)
	}
}

func TestReciprocalRankFusion_tieBreak(t *testing.T) {
	// Construct a case where RRF scores are identical but raw score sums differ.
	// Doc "a": semantic rank 1 (score 1.0), bm25 rank 2 (score 0.0)
	// Doc "b": semantic rank 2 (score 0.0), bm25 rank 1 (score 1.0)
	// With equal weights and k=60 both get 1/61 + 1/62 = same RRF score,
	// but raw sum for a = 1.0, for b = 1.0 — still tied.
	// Instead, make a appear twice in one lane with higher total raw sum.
	semantic := RRFInput{
		Lane: LaneSemantic,
		Results: []RetrievalResult{
			{ID: "a", Score: 10.0},
			{ID: "b", Score: 5.0},
		},
	}
	bm25 := RRFInput{
		Lane: LaneBM25,
		Results: []RetrievalResult{
			{ID: "b", Score: 10.0},
			{ID: "a", Score: 5.0},
		},
	}

	cfg := RRFConfig{K: 60, Weights: map[Lane]float64{
		LaneSemantic: 1.0,
		LaneBM25:     1.0,
	}}

	merged := ReciprocalRankFusion([]RRFInput{semantic, bm25}, cfg)

	// Both have same RRF score (1/61 + 1/62).  Tie-break by raw score sum:
	// a = 10 + 5 = 15, b = 5 + 10 = 15 — still tied.
	// Let's add a third lane where only a appears with a tiny score.
	graph := RRFInput{
		Lane:    LaneGraph,
		Results: []RetrievalResult{{ID: "a", Score: 0.1}},
	}

	merged = ReciprocalRankFusion([]RRFInput{semantic, bm25, graph}, cfg)
	// Now a has higher raw sum (15.1 vs 15) and also slightly higher RRF score.
	if merged[0].ID != "a" {
		t.Errorf("expected tie-break winner 'a', got %s", merged[0].ID)
	}
}

func TestReciprocalRankFusion_dedupesWithinLane(t *testing.T) {
	input := RRFInput{
		Lane: LaneBM25,
		Results: []RetrievalResult{
			{ID: "a", Score: 10},
			{ID: "a", Score: 9},
			{ID: "b", Score: 8},
		},
	}

	merged := ReciprocalRankFusion([]RRFInput{input}, DefaultRRFConfig())

	if len(merged) != 2 {
		t.Fatalf("expected 2 unique candidates, got %d", len(merged))
	}
	if merged[0].ID != "a" || merged[0].SourceRanks[LaneBM25] != 1 {
		t.Fatalf("expected a to keep rank 1, got %+v", merged[0])
	}
	if merged[1].ID != "b" || merged[1].SourceRanks[LaneBM25] != 2 {
		t.Fatalf("expected b to compact to rank 2, got %+v", merged[1])
	}
}

func TestReciprocalRankFusion_missingWeightUsesDefault(t *testing.T) {
	inputs := []RRFInput{
		{Lane: LaneGraph, Results: []RetrievalResult{{ID: "x", Score: 1}}},
	}
	cfg := RRFConfig{K: 60, Weights: map[Lane]float64{LaneBM25: 1}}

	merged := ReciprocalRankFusion(inputs, cfg)

	expected := 0.5 / 61.0
	if len(merged) != 1 || math.Abs(merged[0].RRFScore-expected) > 1e-9 {
		t.Fatalf("expected default graph weight score %.9f, got %+v", expected, merged)
	}
}

func TestReciprocalRankFusion_emptyInput(t *testing.T) {
	merged := ReciprocalRankFusion(nil, DefaultRRFConfig())
	if len(merged) != 0 {
		t.Fatalf("expected 0 results for nil input, got %d", len(merged))
	}
}

func TestReciprocalRankFusion_singleLane(t *testing.T) {
	semantic := RRFInput{
		Lane: LaneSemantic,
		Results: []RetrievalResult{
			{ID: "z", Score: 0.99},
			{ID: "y", Score: 0.50},
			{ID: "x", Score: 0.10},
		},
	}

	cfg := DefaultRRFConfig()
	merged := ReciprocalRankFusion([]RRFInput{semantic}, cfg)

	if len(merged) != 3 {
		t.Fatalf("expected 3 results, got %d", len(merged))
	}
	if merged[0].ID != "z" || merged[1].ID != "y" || merged[2].ID != "x" {
		t.Errorf("unexpected order: %+v", merged)
	}
	for i, m := range merged {
		if m.RRFRank != i+1 {
			t.Errorf("expected rank %d for %s, got %d", i+1, m.ID, m.RRFRank)
		}
	}
}

func TestBudgetToLimit(t *testing.T) {
	tests := []struct {
		budget string
		want   int
	}{
		{"low", 5},
		{"mid", 10},
		{"high", 20},
		{"", 10},
		{"unknown", 10},
	}
	for _, tc := range tests {
		got := BudgetToLimit(tc.budget)
		if got != tc.want {
			t.Errorf("BudgetToLimit(%q) = %d, want %d", tc.budget, got, tc.want)
		}
	}
}
