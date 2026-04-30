package extract

import (
	"math"
	"strings"
	"testing"
)

func TestExtractEntities(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantTypes map[string]int // type -> count
	}{
		{
			name: "person and org",
			text: "Alice Johnson works at NASA and IBM.",
			wantTypes: map[string]int{
				"PERSON": 1, // "Alice Johnson" matched as one sequence
				"ORG":    2,
			},
		},
		{
			name: "email and url",
			text: "Contact support@example.com or visit https://example.com/help.",
			wantTypes: map[string]int{
				"EMAIL": 1,
				"URL":   1,
			},
		},
		{
			name: "date and money",
			text: "On 2024-03-15 we spent $1,234.56 and saved 15%.",
			wantTypes: map[string]int{
				"DATE":   1,
				"MONEY":  1,
				"PERCENT": 1,
			},
		},
		{
			name: "location",
			text: "The meeting is in New York and Tokyo.",
			wantTypes: map[string]int{
				"LOCATION": 2,
			},
		},
		{
			name: "month date",
			text: "Project started on March 12, 2023.",
			wantTypes: map[string]int{
				"DATE": 1,
			},
		},
		{
			name: "relative date",
			text: "We will meet tomorrow and review last week.",
			wantTypes: map[string]int{
				"DATE": 2,
			},
		},
		{
			name: "empty",
			text: "",
			wantTypes: map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEntities(tt.text)
			counts := make(map[string]int)
			for _, m := range got {
				counts[m.Type]++
			}
			for typ, want := range tt.wantTypes {
				if counts[typ] != want {
					t.Errorf("type %s: got %d, want %d", typ, counts[typ], want)
				}
			}
		})
	}
}

func TestExtractEntitiesPositions(t *testing.T) {
	text := "Alice works at NASA."
	mentions := ExtractEntities(text)

	for _, m := range mentions {
		if m.Text == "Alice" {
			if m.Start != 0 || m.End != 5 {
				t.Errorf("Alice position: got %d-%d, want 0-5", m.Start, m.End)
			}
			if m.Type != "PERSON" {
				t.Errorf("Alice type: got %s, want PERSON", m.Type)
			}
		}
	}
}

func TestTextSignalsFromEntities(t *testing.T) {
	mentions := []EntityMention{
		{Text: "Alice", Type: "PERSON"},
		{Text: "NASA", Type: "ORG"},
		{Text: "Alice", Type: "PERSON"}, // duplicate
	}
	signals := TextSignalsFromEntities(mentions)
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(signals))
	}
	if signals[0] != "Alice" || signals[1] != "NASA" {
		t.Errorf("unexpected signals: %v", signals)
	}
}

func TestExtractFacts_Sentences(t *testing.T) {
	text := "The sky is blue. Water is wet."
	facts := ExtractFacts(text)
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
	if !strings.Contains(facts[0].Text, "sky") {
		t.Errorf("expected first fact about sky, got %q", facts[0].Text)
	}
	if facts[0].FactType != "world" {
		t.Errorf("expected fact_type world, got %s", facts[0].FactType)
	}
}

func TestExtractFacts_KeyValue(t *testing.T) {
	text := "name: Alice\nage: 30"
	facts := ExtractFacts(text)
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
	if !strings.Contains(facts[0].Text, "name:") {
		t.Errorf("expected kv fact, got %q", facts[0].Text)
	}
}

func TestExtractFacts_Single(t *testing.T) {
	text := "hello world"
	facts := ExtractFacts(text)
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].Text != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", facts[0].Text)
	}
}

func TestExtractFacts_Timestamp(t *testing.T) {
	text := "Event happened on 2023-07-20."
	facts := ExtractFacts(text)
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
	if facts[0].OccurredStart == nil {
		t.Fatal("expected occurred_start to be set")
	}
	if facts[0].OccurredStart.Year() != 2023 {
		t.Errorf("expected year 2023, got %d", facts[0].OccurredStart.Year())
	}
}

func TestChunkContent(t *testing.T) {
	text := "one two three four five six seven eight nine ten"
	chunks := ChunkContent(text, 3)
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d", len(chunks))
	}
	if chunks[0].Tokens != 3 {
		t.Errorf("expected first chunk 3 tokens, got %d", chunks[0].Tokens)
	}
	if chunks[3].Tokens != 1 {
		t.Errorf("expected last chunk 1 token, got %d", chunks[3].Tokens)
	}
	for _, c := range chunks {
		if c.ChunkID == "" {
			t.Error("expected non-empty chunk_id")
		}
	}
}

func TestChunkContent_DefaultTokens(t *testing.T) {
	// Large text with default chunk size should return 1 chunk
	text := "word "
	text = strings.Repeat(text, 100)
	chunks := ChunkContent(text, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk with default size, got %d", len(chunks))
	}
}

func TestChunkContent_Empty(t *testing.T) {
	chunks := ChunkContent("", 10)
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty text, got %d", len(chunks))
	}
}

func TestCreateSemanticLinks(t *testing.T) {
	ids := []string{"a", "b", "c"}
	embeddings := [][]float32{
		{1, 0, 0},
		{1, 0, 0},
		{0, 1, 0},
	}
	links := CreateSemanticLinks(ids, embeddings, 0.99)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].FromID != "a" || links[0].ToID != "b" {
		t.Errorf("unexpected link: %+v", links[0])
	}
	if math.Abs(links[0].Score-1.0) > 1e-6 {
		t.Errorf("expected score ~1.0, got %f", links[0].Score)
	}
}

func TestCreateSemanticLinks_None(t *testing.T) {
	ids := []string{"a", "b"}
	embeddings := [][]float32{
		{1, 0},
		{0, 1},
	}
	links := CreateSemanticLinks(ids, embeddings, 0.99)
	if len(links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(links))
	}
}

func TestCreateSemanticLinks_DefaultThreshold(t *testing.T) {
	ids := []string{"a", "b"}
	embeddings := [][]float32{
		{1, 0, 0},
		{0.9, 0.1, 0},
	}
	links := CreateSemanticLinks(ids, embeddings, 0)
	if len(links) != 1 {
		t.Fatalf("expected 1 link with default threshold, got %d", len(links))
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2, 3}
	sim := cosineSimilarity(a, b)
	if math.Abs(sim-1.0) > 1e-6 {
		t.Errorf("expected 1.0, got %f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	sim := cosineSimilarity(a, b)
	if math.Abs(sim-0.0) > 1e-6 {
		t.Errorf("expected 0.0, got %f", sim)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	sim := cosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("expected 0 for different lengths, got %f", sim)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{1, 2}
	sim := cosineSimilarity(a, b)
	if sim != 0 {
		t.Errorf("expected 0 for zero vector, got %f", sim)
	}
}
