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
	fixtureDir := "../../../eval-workspace/memory-recall/fixtures/v1"
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read dir: %v\n", err)
		os.Exit(1)
	}

	allPass := true
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(fixtureDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
			allPass = false
			continue
		}
		var fixture Fixture
		if err := json.Unmarshal(data, &fixture); err != nil {
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", path, err)
			allPass = false
			continue
		}

		pass := verifyFixture(fixture, path)
		if !pass {
			allPass = false
		}
	}

	if !allPass {
		os.Exit(1)
	}
	fmt.Println("\nAll fixtures passed!")
}

func verifyFixture(f Fixture, path string) bool {
	k := f.K
	if k <= 0 {
		k = 60
	}
	weights := make(map[retrieve.Lane]float64)
	for laneStr, w := range f.Weights {
		weights[retrieve.Lane(laneStr)] = w
	}
	config := retrieve.RRFConfig{K: k, Weights: weights}

	var inputs []retrieve.RRFInput
	laneNames := make([]string, 0, len(f.Lanes))
	for name := range f.Lanes {
		laneNames = append(laneNames, name)
	}
	sort.Strings(laneNames)

	for _, laneName := range laneNames {
		results := f.Lanes[laneName]
		rrfResults := make([]retrieve.RetrievalResult, len(results))
		for i, r := range results {
			rrfResults[i] = retrieve.RetrievalResult{ID: r.ID, Score: r.Score}
		}
		inputs = append(inputs, retrieve.RRFInput{
			Lane:    retrieve.Lane(laneName),
			Results: rrfResults,
		})
	}

	merged := retrieve.ReciprocalRankFusion(inputs, config)

	pass := true
	if len(merged) != len(f.ExpectedTopK) {
		fmt.Printf("FAIL %s: result count %d != expected %d\n", path, len(merged), len(f.ExpectedTopK))
		pass = false
	}
	for i, exp := range f.ExpectedTopK {
		if i >= len(merged) {
			break
		}
		actual := merged[i]
		if actual.ID != exp.ID || actual.RRFRank != exp.Rank {
			fmt.Printf("FAIL %s: rank %d expected %s (rank %d), got %s (rank %d, score %.6f)\n",
				path, i+1, exp.ID, exp.Rank, actual.ID, actual.RRFRank, actual.RRFScore)
			pass = false
		}
	}
	if pass {
		fmt.Printf("PASS %s (%s)\n", path, f.Name)
	} else {
		fmt.Printf("  Actual ranking for %s:\n", f.Name)
		for _, m := range merged {
			fmt.Printf("    %s  rank=%d  score=%.6f\n", m.ID, m.RRFRank, m.RRFScore)
		}
	}
	return pass
}
