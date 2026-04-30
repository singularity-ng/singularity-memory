package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/singularity-ng/singularity-memory/go/internal/retrieve"
)

const fixturesDir = "../eval-workspace/memory-recall/fixtures/v1"

const tolerance = 1e-9

type Fixture struct {
	Name         string                     `json:"name"`
	Description  string                     `json:"description"`
	Query        string                     `json:"query"`
	K            int                        `json:"k"`
	Weights      map[string]float64         `json:"weights"`
	Lanes        map[string][]FixtureResult `json:"lanes"`
	ExpectedTopK []FixtureExpected          `json:"expected_top_k"`
}

type FixtureResult struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

type FixtureExpected struct {
	ID   string `json:"id"`
	Rank int    `json:"rank"`
}

func main() {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read fixtures dir: %v\n", err)
		os.Exit(1)
	}

	var fixtures []Fixture
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(fixturesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read %s: %v\n", path, err)
			os.Exit(1)
		}
		var f Fixture
		if err := json.Unmarshal(data, &f); err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse %s: %v\n", path, err)
			os.Exit(1)
		}
		fixtures = append(fixtures, f)
	}

	sort.Slice(fixtures, func(i, j int) bool {
		return fixtures[i].Name < fixtures[j].Name
	})

	fmt.Printf("%-30s | %-11s | %-8s | %-9s | %-9s | %s\n",
		"fixture", "exact_top10", "recall@5", "recall@10", "recall@20", "pass/fail")
	fmt.Println(strings.Repeat("-", 95))

	allPass := true
	for _, f := range fixtures {
		pass := runFixture(f)
		if !pass {
			allPass = false
		}
	}

	if !allPass {
		os.Exit(1)
	}
}

func runFixture(f Fixture) bool {
	config := retrieve.RRFConfig{
		K:       f.K,
		Weights: make(map[retrieve.Lane]float64),
	}
	if config.K <= 0 {
		config.K = 60
	}
	for laneName, w := range f.Weights {
		config.Weights[retrieve.Lane(laneName)] = w
	}

	var inputs []retrieve.RRFInput
	for laneName, results := range f.Lanes {
		var rr []retrieve.RetrievalResult
		for _, r := range results {
			rr = append(rr, retrieve.RetrievalResult{ID: r.ID, Score: r.Score})
		}
		inputs = append(inputs, retrieve.RRFInput{
			Lane:    retrieve.Lane(laneName),
			Results: rr,
		})
	}

	merged := retrieve.ReciprocalRankFusion(inputs, config)

	// Build expected id -> rank map.
	expectedRank := make(map[string]int, len(f.ExpectedTopK))
	expectedIDs := make([]string, 0, len(f.ExpectedTopK))
	for _, e := range f.ExpectedTopK {
		expectedRank[e.ID] = e.Rank
		expectedIDs = append(expectedIDs, e.ID)
	}

	// Exact top-10 match: compare IDs of top 10 merged results with expected top 10.
	topN := 10
	if len(merged) < topN {
		topN = len(merged)
	}
	if len(expectedIDs) < topN {
		topN = len(expectedIDs)
	}
	exactTop10 := true
	for i := 0; i < topN; i++ {
		if merged[i].ID != expectedIDs[i] {
			exactTop10 = false
			break
		}
	}

	// Compute recall@k for k in {5,10,20}.
	// Here "recall" means: of the expected IDs, how many appear in the top-k merged results.
	recall := func(k int) float64 {
		if k > len(merged) {
			k = len(merged)
		}
		found := 0
		for i := 0; i < k; i++ {
			if _, ok := expectedRank[merged[i].ID]; ok {
				found++
			}
		}
		if len(expectedRank) == 0 {
			return 1.0
		}
		return float64(found) / float64(len(expectedRank))
	}

	r5 := recall(5)
	r10 := recall(10)
	r20 := recall(20)

	// The pass condition: exact top-10 ordering must match.
	// For fixtures with >10 expected documents, recall@10 cannot be 1.0 by definition,
	// so we gate only on exact_top10 parity (the primary RRF correctness signal).
	pass := exactTop10
	status := "PASS"
	if !pass {
		status = "FAIL"
	}

	fmt.Printf("%-30s | %-11v | %-8.4f | %-9.4f | %-9.4f | %s\n",
		f.Name, exactTop10, r5, r10, r20, status)

	return pass
}
