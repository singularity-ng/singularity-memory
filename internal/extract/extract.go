package extract

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Entity types
// ---------------------------------------------------------------------------

type EntityMention struct {
	Text  string `json:"text"`
	Type  string `json:"type"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// ---------------------------------------------------------------------------
// Fact types
// ---------------------------------------------------------------------------

type Fact struct {
	Text          string     `json:"text"`
	FactType      string     `json:"fact_type"`
	OccurredStart *time.Time `json:"occurred_start,omitempty"`
	OccurredEnd   *time.Time `json:"occurred_end,omitempty"`
}

// ---------------------------------------------------------------------------
// Semantic link types
// ---------------------------------------------------------------------------

type SemanticLink struct {
	FromID string  `json:"from_id"`
	ToID   string  `json:"to_id"`
	Score  float64 `json:"score"`
}

// ---------------------------------------------------------------------------
// Regex entity extraction patterns
// ---------------------------------------------------------------------------

var (
	// PERSON: capitalized word sequences (e.g. "John Smith", "Alice")
	personRe = regexp.MustCompile(`\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)*\b`)

	// ORG: all-caps sequences of length >= 2 (e.g. "NASA", "IBM")
	orgRe = regexp.MustCompile(`\b[A-Z]{2,}(?:\s+[A-Z]{2,})*\b`)

	// LOCATION: known location keywords + capitalized sequences that look like places
	locationRe = regexp.MustCompile(`\b(?:New York|San Francisco|Los Angeles|Chicago|Boston|Seattle|Austin|London|Paris|Berlin|Tokyo|Sydney|Beijing|Mumbai|Cairo|Rio de Janeiro|Toronto|Vancouver|Montreal|Dublin|Amsterdam|Singapore|Hong Kong|Dubai|Barcelona|Madrid|Rome|Milan|Vienna|Zurich|Stockholm|Oslo|Copenhagen|Helsinki|Moscow|Saint Petersburg|Istanbul|Jerusalem|Cape Town|Johannesburg|Nairobi|Lagos|Cairo|Alexandria|Casablanca|Mexico City|São Paulo|Buenos Aires|Santiago|Lima|Bogotá|Caracas|Quito|La Paz|Asunción|Montevideo|Panama City|San José|Guatemala City|Tegucigalpa|Managua|San Salvador|Port-au-Prince|Havana|Santo Domingo|San Juan|Port of Spain|Bridgetown|Kingston|Nassau|Belmopan|Belize City)\b`)

	// DATE: ISO dates (YYYY-MM-DD, YYYY/MM/DD)
	dateISORe = regexp.MustCompile(`\b\d{4}[-/]\d{2}[-/]\d{2}\b`)

	// DATE: month names with optional day and year
	dateMonthRe = regexp.MustCompile(`\b(?:January|February|March|April|May|June|July|August|September|October|November|December)(?:\s+\d{1,2}(?:,\s+\d{4})?)?\b`)

	// DATE: relative dates
	dateRelativeRe = regexp.MustCompile(`\b(?:today|yesterday|tomorrow|last\s+(?:week|month|year)|next\s+(?:week|month|year))\b`)

	// MONEY: currency amounts
	moneyRe = regexp.MustCompile(`(?:\$|€|£|¥|USD|EUR|GBP|JPY|CNY)\s*[\d,]+(?:\.\d{2})?|[\d,]+(?:\.\d{2})?\s*(?:USD|EUR|GBP|JPY|CNY|\$|€|£|¥)`)

	// PERCENT: percentage values
	percentRe = regexp.MustCompile(`\d+(?:\.\d+)?%`)

	// EMAIL
	emailRe = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)

	// URL
	urlRe = regexp.MustCompile(`\bhttps?://[^\s<>"{}|\\^\[\` + "`" + `]+\b`)
)

// ---------------------------------------------------------------------------
// ExtractEntities extracts structured entity mentions from text.
// ---------------------------------------------------------------------------

func ExtractEntities(text string) []EntityMention {
	var mentions []EntityMention
	seen := make(map[string]struct{})

	// Helper to add mentions without duplicates
	add := func(matches [][]int, typ string) {
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			start, end := m[0], m[1]
			val := text[start:end]
			key := typ + ":" + val
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			mentions = append(mentions, EntityMention{
				Text:  val,
				Type:  typ,
				Start: start,
				End:   end,
			})
		}
	}

	add(personRe.FindAllStringIndex(text, -1), "PERSON")
	add(orgRe.FindAllStringIndex(text, -1), "ORG")
	add(locationRe.FindAllStringIndex(text, -1), "LOCATION")
	add(dateISORe.FindAllStringIndex(text, -1), "DATE")
	add(dateMonthRe.FindAllStringIndex(text, -1), "DATE")
	add(dateRelativeRe.FindAllStringIndex(text, -1), "DATE")
	add(moneyRe.FindAllStringIndex(text, -1), "MONEY")
	add(percentRe.FindAllStringIndex(text, -1), "PERCENT")
	add(emailRe.FindAllStringIndex(text, -1), "EMAIL")
	add(urlRe.FindAllStringIndex(text, -1), "URL")

	return mentions
}

// ---------------------------------------------------------------------------
// TextSignalsFromEntities returns unique entity texts for BM25 enrichment.
// ---------------------------------------------------------------------------

func TextSignalsFromEntities(mentions []EntityMention) []string {
	seen := make(map[string]struct{})
	var signals []string
	for _, m := range mentions {
		if _, ok := seen[m.Text]; !ok {
			seen[m.Text] = struct{}{}
			signals = append(signals, m.Text)
		}
	}
	return signals
}

// ---------------------------------------------------------------------------
// ExtractFacts performs simple fact extraction.
// ---------------------------------------------------------------------------

func ExtractFacts(text string) []Fact {
	facts := extractSimpleFacts(text)
	occurredStart, occurredEnd := extractTimestamps(text)

	var result []Fact
	for _, f := range facts {
		result = append(result, Fact{
			Text:          f,
			FactType:      "world",
			OccurredStart: occurredStart,
			OccurredEnd:   occurredEnd,
		})
	}
	return result
}

func extractSimpleFacts(text string) []string {
	// If content looks like key-value (contains ':' or '='), extract as structured facts.
	if strings.Contains(text, ":") || strings.Contains(text, "=") {
		lines := strings.Split(text, "\n")
		var facts []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if idx := strings.Index(line, ":"); idx > 0 {
				facts = append(facts, line)
			} else if idx := strings.Index(line, "="); idx > 0 {
				facts = append(facts, line)
			} else {
				// Fall through to sentence splitting for non-kv lines
				sentences := splitSentences(line)
				facts = append(facts, sentences...)
			}
		}
		if len(facts) > 0 {
			return facts
		}
	}

	sentences := splitSentences(text)
	if len(sentences) == 0 {
		return []string{text}
	}
	return sentences
}

func splitSentences(text string) []string {
	// Split on sentence-ending punctuation followed by space or end of string.
	re := regexp.MustCompile(`[.!?]+\s+`)
	parts := re.Split(text, -1)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// extractTimestamps extracts date/timestamp information from text.
func extractTimestamps(text string) (*time.Time, *time.Time) {
	// Try ISO date first
	if m := dateISORe.FindString(text); m != "" {
		for _, layout := range []string{"2006-01-02", "2006/01/02"} {
			if t, err := time.Parse(layout, m); err == nil {
				return &t, &t
			}
		}
	}

	// Try month name patterns
	if m := dateMonthRe.FindString(text); m != "" {
		layouts := []string{
			"January 2, 2006",
			"January 2 2006",
			"January 2006",
			"January 2",
			"January",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, m); err == nil {
				return &t, &t
			}
		}
	}

	return nil, nil
}

// ---------------------------------------------------------------------------
// ChunkContent splits content by token count using word-based tokenizer.
// Each chunk gets a unique chunk_id (UUID).
// ---------------------------------------------------------------------------

func ChunkContent(content string, tokensPerChunk int) []struct {
	ChunkID string `json:"chunk_id"`
	Text    string `json:"text"`
	Tokens  int    `json:"tokens"`
} {
	if tokensPerChunk <= 0 {
		tokensPerChunk = 2000
	}

	words := strings.Fields(content)
	if len(words) == 0 {
		return nil
	}

	var chunks []struct {
		ChunkID string `json:"chunk_id"`
		Text    string `json:"text"`
		Tokens  int    `json:"tokens"`
	}

	var current []string
	currentTokens := 0

	for _, w := range words {
		if currentTokens > 0 && currentTokens+1 > tokensPerChunk {
			chunks = append(chunks, struct {
				ChunkID string `json:"chunk_id"`
				Text    string `json:"text"`
				Tokens  int    `json:"tokens"`
			}{
				ChunkID: uuid.New().String(),
				Text:    strings.Join(current, " "),
				Tokens:  currentTokens,
			})
			current = nil
			currentTokens = 0
		}
		current = append(current, w)
		currentTokens++
	}

	if len(current) > 0 {
		chunks = append(chunks, struct {
			ChunkID string `json:"chunk_id"`
			Text    string `json:"text"`
			Tokens  int    `json:"tokens"`
		}{
			ChunkID: uuid.New().String(),
			Text:    strings.Join(current, " "),
			Tokens:  currentTokens,
		})
	}

	return chunks
}

// ---------------------------------------------------------------------------
// CreateSemanticLinks generates semantic links between items with similar
// embeddings (cosine similarity > threshold). Score = 1 - cosine_distance.
// ---------------------------------------------------------------------------

func CreateSemanticLinks(ids []string, embeddings [][]float32, threshold float64) []SemanticLink {
	if threshold <= 0 {
		threshold = 0.85
	}

	var links []SemanticLink
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			sim := cosineSimilarity(embeddings[i], embeddings[j])
			if sim >= threshold {
				links = append(links, SemanticLink{
					FromID: ids[i],
					ToID:   ids[j],
					Score:  sim,
				})
			}
		}
	}
	return links
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
