package retrieve

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

// ---------------------------------------------------------------------------
// Retrieval result (rich version used by all lanes)
// ---------------------------------------------------------------------------

// FullRetrievalResult carries all fields that any lane may populate.
type FullRetrievalResult struct {
	ID                string
	Text              string
	FactType          string
	Context           *string
	EventDate         *time.Time
	OccurredStart     *time.Time
	OccurredEnd       *time.Time
	MentionedAt       *time.Time
	DocumentID        *string
	ChunkID           *string
	Tags              []string
	Metadata          map[string]any
	ProofCount        int
	Entities          []string // entity canonical names from unit_entities join
	SourceFactIDs     []string // source fact IDs for observation tracking
	Similarity        float64  // semantic lane
	BM25Score         float64  // BM25 lane (converted to positive)
	Activation        float64  // graph lane
	TemporalScore     float64  // temporal lane (combined)
	TemporalProximity float64  // temporal lane (raw proximity)
}

// ToRRFResult converts a full result into the slim RRF input.
func (r *FullRetrievalResult) ToRRFResult(score float64) RetrievalResult {
	return RetrievalResult{ID: r.ID, Score: score}
}

// ---------------------------------------------------------------------------
// Tag filter helpers
// ---------------------------------------------------------------------------

// TagsMatchMode controls how tags are matched.
type TagsMatchMode string

const (
	TagsMatchAny       TagsMatchMode = "any"
	TagsMatchAll       TagsMatchMode = "all"
	TagsMatchAnyStrict TagsMatchMode = "any_strict"
	TagsMatchAllStrict TagsMatchMode = "all_strict"
)

// BuildTagsClause returns a SQL fragment and the parameter value for tags filtering.
// If tags is empty it returns empty strings and the original offset.
func BuildTagsClause(tags []string, paramOffset int, tableAlias string, match TagsMatchMode) (string, any, int) {
	if len(tags) == 0 {
		return "", nil, paramOffset
	}

	col := "tags"
	if tableAlias != "" {
		col = tableAlias + ".tags"
	}

	var operator string
	var includeUntagged bool

	switch match {
	case TagsMatchAll, TagsMatchAllStrict:
		operator = "@>"
	default:
		operator = "&&"
	}

	switch match {
	case TagsMatchAny, TagsMatchAll:
		includeUntagged = true
	default:
		includeUntagged = false
	}

	placeholder := fmt.Sprintf("$%d", paramOffset)
	var clause string
	if includeUntagged {
		clause = fmt.Sprintf("AND (%s IS NULL OR %s = '{}' OR %s %s %s)", col, col, col, operator, placeholder)
	} else {
		clause = fmt.Sprintf("AND %s IS NOT NULL AND %s != '{}' AND %s %s %s", col, col, col, operator, placeholder)
	}

	return clause, tags, paramOffset + 1
}

// ---------------------------------------------------------------------------
// Store interface (minimal subset needed for retrieval)
// ---------------------------------------------------------------------------

// Querier is satisfied by *pgxpool.Pool or pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ---------------------------------------------------------------------------
// Semantic retrieval
// ---------------------------------------------------------------------------

// SemanticRetrieval performs approximate kNN via vchordrq.
// It over-fetches by 5x (min 100) and trims in Go.
func SemanticRetrieval(
	ctx context.Context,
	q Querier,
	schema func(string) string,
	bankID string,
	queryEmbedding []float32,
	factTypes []string,
	tags []string,
	tagsMatch TagsMatchMode,
	limit int,
) ([]*FullRetrievalResult, error) {
	if len(queryEmbedding) == 0 || len(factTypes) == 0 {
		return nil, nil
	}

	hnswFetch := max(limit*5, 100)

	vec := pgvector.NewVector(queryEmbedding)

	cols := "id, text, context, event_date, occurred_start, occurred_end, mentioned_at, fact_type, document_id, chunk_id, tags, metadata, proof_count"

	tagsClause, tagsParam, _ := BuildTagsClause(tags, 3, "", tagsMatch)

	var arms []string
	for _, ft := range factTypes {
		// Safe to inline ft because it comes from a controlled enum.
		arm := fmt.Sprintf(
			"(SELECT %s, 1 - (embedding <=> $1::vector) AS similarity, NULL::float AS bm25_score FROM %s WHERE bank_id = $2 AND fact_type = '%s' AND embedding IS NOT NULL AND (1 - (embedding <=> $1::vector)) >= 0.3 %s ORDER BY embedding <=> $1::vector LIMIT %d)",
			cols, schema("memory_units"), ft, tagsClause, hnswFetch,
		)
		arms = append(arms, arm)
	}

	query := strings.Join(arms, "\nUNION ALL\n")

	params := []any{vec, bankID}
	if tagsParam != nil {
		params = append(params, tagsParam)
	}

	rows, err := q.Query(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("semantic retrieval: %w", err)
	}
	defer rows.Close()

	results, err := scanFullResults(rows, limit, func(r *FullRetrievalResult) float64 { return r.Similarity })
	if err != nil {
		return nil, err
	}

	return results, nil
}

// ---------------------------------------------------------------------------
// BM25 retrieval
// ---------------------------------------------------------------------------

// BM25Retrieval performs VectorChord-BM25 keyword search.
// BM25 ranks are negative (lower = more relevant); we convert to positive
// scores with -bm25_rank so higher is better for RRF.
func BM25Retrieval(
	ctx context.Context,
	q Querier,
	schema func(string) string,
	bankID string,
	queryText string,
	factTypes []string,
	tags []string,
	tagsMatch TagsMatchMode,
	limit int,
) ([]*FullRetrievalResult, error) {
	if strings.TrimSpace(queryText) == "" || len(factTypes) == 0 {
		return nil, nil
	}

	tokens := tokenizeQuery(queryText)
	if len(tokens) == 0 {
		return nil, nil
	}

	cols := "id, text, context, event_date, occurred_start, occurred_end, mentioned_at, fact_type, document_id, chunk_id, tags, metadata, proof_count"

	tagsClause, tagsParam, _ := BuildTagsClause(tags, 4, "", tagsMatch)

	bm25ScoreExpr := fmt.Sprintf(
		"search_vector <&> to_bm25query('%s'::regclass, tokenize($3, 'llmlingua2')::bm25_catalog.bm25vector)",
		schema("idx_memory_units_text_search"),
	)
	bm25OrderBy := bm25ScoreExpr + " ASC"

	var arms []string
	for _, ft := range factTypes {
		arm := fmt.Sprintf(
			"(SELECT %s, NULL::float AS similarity, %s AS bm25_score FROM %s WHERE bank_id = $1 AND fact_type = '%s' %s ORDER BY %s LIMIT $2)",
			cols, bm25ScoreExpr, schema("memory_units"), ft, tagsClause, bm25OrderBy,
		)
		arms = append(arms, arm)
	}

	query := strings.Join(arms, "\nUNION ALL\n")

	params := []any{bankID, limit, queryText}
	if tagsParam != nil {
		params = append(params, tagsParam)
	}

	rows, err := q.Query(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("bm25 retrieval: %w", err)
	}
	defer rows.Close()

	results, err := scanFullResults(rows, 0, nil) // 0 = no per-fact-type trim
	if err != nil {
		return nil, err
	}

	// Convert negative BM25 ranks to positive scores.
	for _, r := range results {
		r.BM25Score = -r.BM25Score
	}

	// Global trim to limit.
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// ---------------------------------------------------------------------------
// Combined semantic + BM25
// ---------------------------------------------------------------------------

// CombinedSemanticBM25 runs semantic and BM25 in one UNION ALL query when
// both are enabled, otherwise delegates to the individual function.
// Results are deduplicated per lane (keeping the highest score per unit_id).
func CombinedSemanticBM25(
	ctx context.Context,
	q Querier,
	schema func(string) string,
	bankID string,
	queryEmbedding []float32,
	queryText string,
	factTypes []string,
	tags []string,
	tagsMatch TagsMatchMode,
	limit int,
) (semanticResults []*FullRetrievalResult, bm25Results []*FullRetrievalResult, err error) {
	semanticEnabled := len(queryEmbedding) > 0
	tokens := tokenizeQuery(queryText)
	bm25Enabled := len(tokens) > 0

	if !semanticEnabled && !bm25Enabled {
		return nil, nil, nil
	}

	if !semanticEnabled {
		bm25Results, err = BM25Retrieval(ctx, q, schema, bankID, queryText, factTypes, tags, tagsMatch, limit)
		return nil, bm25Results, err
	}

	if !bm25Enabled {
		semanticResults, err = SemanticRetrieval(ctx, q, schema, bankID, queryEmbedding, factTypes, tags, tagsMatch, limit)
		return semanticResults, nil, err
	}

	// Both enabled — build a single UNION ALL query.
	hnswFetch := max(limit*5, 100)
	vec := pgvector.NewVector(queryEmbedding)

	cols := "id, text, context, event_date, occurred_start, occurred_end, mentioned_at, fact_type, document_id, chunk_id, tags, metadata, proof_count"

	// Parameter layout:
	// $1 = embedding, $2 = bank_id, $3 = bm25_limit, $4 = bm25_text
	// tags at $5 (if present)
	tagsClause, tagsParam, _ := BuildTagsClause(tags, 5, "", tagsMatch)

	bm25ScoreExpr := fmt.Sprintf(
		"search_vector <&> to_bm25query('%s'::regclass, tokenize($4, 'llmlingua2')::bm25_catalog.bm25vector)",
		schema("idx_memory_units_text_search"),
	)
	bm25OrderBy := bm25ScoreExpr + " ASC"

	var arms []string
	for _, ft := range factTypes {
		semArm := fmt.Sprintf(
			"(SELECT %s, 1 - (embedding <=> $1::vector) AS similarity, NULL::float AS bm25_score, 'semantic' AS source FROM %s WHERE bank_id = $2 AND fact_type = '%s' AND embedding IS NOT NULL AND (1 - (embedding <=> $1::vector)) >= 0.3 %s ORDER BY embedding <=> $1::vector LIMIT %d)",
			cols, schema("memory_units"), ft, tagsClause, hnswFetch,
		)
		arms = append(arms, semArm)
	}

	for _, ft := range factTypes {
		bm25Arm := fmt.Sprintf(
			"(SELECT %s, NULL::float AS similarity, %s AS bm25_score, 'bm25' AS source FROM %s WHERE bank_id = $2 AND fact_type = '%s' %s ORDER BY %s LIMIT $3)",
			cols, bm25ScoreExpr, schema("memory_units"), ft, tagsClause, bm25OrderBy,
		)
		arms = append(arms, bm25Arm)
	}

	query := strings.Join(arms, "\nUNION ALL\n")

	params := []any{vec, bankID, limit, queryText}
	if tagsParam != nil {
		params = append(params, tagsParam)
	}

	rows, err := q.Query(ctx, query, params...)
	if err != nil {
		return nil, nil, fmt.Errorf("combined retrieval: %w", err)
	}
	defer rows.Close()

	// Read all rows, split by source, trim semantic per fact_type.
	semByFT := make(map[string][]*FullRetrievalResult)
	bm25ByFT := make(map[string][]*FullRetrievalResult)
	for _, ft := range factTypes {
		semByFT[ft] = nil
		bm25ByFT[ft] = nil
	}

	for rows.Next() {
		var r FullRetrievalResult
		var source string
		var similarity, bm25Score *float64
		var eventDate, occurredStart, occurredEnd, mentionedAt *time.Time
		var docID, chunkID, contextStr *string
		var tagsArr []string
		var metadataBytes []byte

		err := rows.Scan(
			&r.ID, &r.Text, &contextStr, &eventDate, &occurredStart, &occurredEnd, &mentionedAt,
			&r.FactType, &docID, &chunkID, &tagsArr, &metadataBytes, &r.ProofCount,
			&similarity, &bm25Score, &source,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("scan combined row: %w", err)
		}

		if contextStr != nil {
			r.Context = contextStr
		}
		r.EventDate = eventDate
		r.OccurredStart = occurredStart
		r.OccurredEnd = occurredEnd
		r.MentionedAt = mentionedAt
		if docID != nil {
			r.DocumentID = docID
		}
		if chunkID != nil {
			r.ChunkID = chunkID
		}
		r.Tags = tagsArr
		if len(metadataBytes) > 0 {
			_ = json.Unmarshal(metadataBytes, &r.Metadata)
		}

		if source == "semantic" && similarity != nil {
			r.Similarity = *similarity
		}
		if source == "bm25" && bm25Score != nil {
			r.BM25Score = -*bm25Score // convert negative rank to positive score
		}

		if source == "semantic" {
			if len(semByFT[r.FactType]) < limit {
				semByFT[r.FactType] = append(semByFT[r.FactType], &r)
			}
		} else {
			bm25ByFT[r.FactType] = append(bm25ByFT[r.FactType], &r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("combined rows: %w", err)
	}

	// Flatten and dedupe per lane.
	semanticResults = flattenAndDedupe(semByFT, func(r *FullRetrievalResult) float64 { return r.Similarity })
	bm25Results = flattenAndDedupe(bm25ByFT, func(r *FullRetrievalResult) float64 { return r.BM25Score })

	// Global BM25 trim.
	if len(bm25Results) > limit {
		bm25Results = bm25Results[:limit]
	}

	return semanticResults, bm25Results, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func tokenizeQuery(text string) []string {
	// Strip punctuation, lowercase, split on whitespace.
	cleaned := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' {
			return r
		}
		return ' '
	}, text)
	cleaned = strings.ToLower(strings.TrimSpace(cleaned))
	if cleaned == "" {
		return nil
	}
	parts := strings.Fields(cleaned)
	// Deduplicate while preserving order.
	seen := make(map[string]struct{}, len(parts))
	var out []string
	for _, p := range parts {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

func scanFullResults(rows pgx.Rows, perFactTypeLimit int, scoreFn func(*FullRetrievalResult) float64) ([]*FullRetrievalResult, error) {
	byFT := make(map[string][]*FullRetrievalResult)

	for rows.Next() {
		var r FullRetrievalResult
		var similarity, bm25Score *float64
		var eventDate, occurredStart, occurredEnd, mentionedAt *time.Time
		var docID, chunkID, contextStr *string
		var tagsArr []string
		var metadataBytes []byte

		err := rows.Scan(
			&r.ID, &r.Text, &contextStr, &eventDate, &occurredStart, &occurredEnd, &mentionedAt,
			&r.FactType, &docID, &chunkID, &tagsArr, &metadataBytes, &r.ProofCount,
			&similarity, &bm25Score,
		)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		if contextStr != nil {
			r.Context = contextStr
		}
		r.EventDate = eventDate
		r.OccurredStart = occurredStart
		r.OccurredEnd = occurredEnd
		r.MentionedAt = mentionedAt
		if docID != nil {
			r.DocumentID = docID
		}
		if chunkID != nil {
			r.ChunkID = chunkID
		}
		r.Tags = tagsArr
		if len(metadataBytes) > 0 {
			_ = json.Unmarshal(metadataBytes, &r.Metadata)
		}
		if similarity != nil {
			r.Similarity = *similarity
		}
		if bm25Score != nil {
			r.BM25Score = *bm25Score
		}

		if perFactTypeLimit > 0 {
			if len(byFT[r.FactType]) < perFactTypeLimit {
				byFT[r.FactType] = append(byFT[r.FactType], &r)
			}
		} else {
			byFT[r.FactType] = append(byFT[r.FactType], &r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return flattenAndDedupe(byFT, scoreFn), nil
}

func flattenAndDedupe(byFT map[string][]*FullRetrievalResult, scoreFn func(*FullRetrievalResult) float64) []*FullRetrievalResult {
	best := make(map[string]*FullRetrievalResult)
	for _, list := range byFT {
		for _, r := range list {
			if existing, ok := best[r.ID]; !ok || scoreFn(r) > scoreFn(existing) {
				best[r.ID] = r
			}
		}
	}

	out := make([]*FullRetrievalResult, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}

	// Sort by score descending.
	if scoreFn != nil {
		sort.Slice(out, func(i, j int) bool {
			return scoreFn(out[i]) > scoreFn(out[j])
		})
	}

	return out
}
