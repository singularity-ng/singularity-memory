package retrieve

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Graph retrieval via relational link expansion
// ---------------------------------------------------------------------------

// GraphRetrieval expands from semantic seed unit IDs through memory_links.
// It runs entity co-occurrence (LATERAL capped), semantic bidirectional,
// and causal chain traversal in a single CTE query for non-observation
// fact types, or separate queries for observations.
func GraphRetrieval(
	ctx context.Context,
	q Querier,
	schema func(string) string,
	bankID string,
	seedUnitIDs []string,
	factTypes []string,
	tags []string,
	tagsMatch TagsMatchMode,
	limit int,
	maxIterations int,
) ([]*FullRetrievalResult, error) {
	if len(seedUnitIDs) == 0 || len(factTypes) == 0 {
		return nil, nil
	}

	if maxIterations <= 0 {
		maxIterations = 5
	}

	// For simplicity we run one query per fact type (matching Python's
	// parallel task structure but sequentially in Go).  Each query combines
	// entity, semantic, and causal expansions when possible.
	var allResults []*FullRetrievalResult
	seen := make(map[string]struct{})

	for _, ft := range factTypes {
		results, err := graphRetrieveForFactType(ctx, q, schema, bankID, seedUnitIDs, ft, tags, tagsMatch, limit)
		if err != nil {
			return nil, fmt.Errorf("graph retrieval for %s: %w", ft, err)
		}
		for _, r := range results {
			if _, ok := seen[r.ID]; ok {
				continue
			}
			seen[r.ID] = struct{}{}
			allResults = append(allResults, r)
		}
	}

	// Sort by activation (score) descending and trim to limit.
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Activation > allResults[j].Activation
	})
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	return allResults, nil
}

func graphRetrieveForFactType(
	ctx context.Context,
	q Querier,
	schema func(string) string,
	bankID string,
	seedUnitIDs []string,
	factType string,
	tags []string,
	tagsMatch TagsMatchMode,
	limit int,
) ([]*FullRetrievalResult, error) {
	mu := schema("memory_units")
	ml := schema("memory_links")
	ue := schema("unit_entities")

	perEntityLimit := 200 // matches Python default config.link_expansion_per_entity_limit
	causalThreshold := 0.3

	// Build entity CTE with LATERAL cap.
	entityCTE := fmt.Sprintf(`
seed_entities AS (
    SELECT DISTINCT ue.entity_id
    FROM %s ue
    WHERE ue.unit_id = ANY($1::uuid[])
),
entity_expanded AS (
    SELECT mu.id, mu.text, mu.context, mu.event_date, mu.occurred_start,
           mu.occurred_end, mu.mentioned_at, mu.fact_type, mu.document_id,
           mu.chunk_id, mu.tags, mu.metadata, mu.proof_count,
           COUNT(DISTINCT se.entity_id)::float AS score,
           'entity'::text AS source
    FROM seed_entities se
    CROSS JOIN LATERAL (
        SELECT ue_target.unit_id
        FROM %s ue_target
        WHERE ue_target.entity_id = se.entity_id
          AND ue_target.unit_id != ALL($1::uuid[])
        ORDER BY ue_target.unit_id DESC
        LIMIT %d
    ) t
    JOIN %s mu ON mu.id = t.unit_id
    WHERE mu.fact_type = $2
      AND mu.bank_id = $5
    GROUP BY mu.id
    ORDER BY score DESC
    LIMIT $3
)`, ue, ue, perEntityLimit, mu)

	semanticCTE := fmt.Sprintf(`
semantic_expanded AS (
    SELECT
        id, text, context, event_date, occurred_start,
        occurred_end, mentioned_at, fact_type, document_id,
        chunk_id, tags, metadata, proof_count,
        MAX(weight) AS score,
        'semantic'::text AS source
    FROM (
        SELECT mu.id, mu.text, mu.context, mu.event_date, mu.occurred_start,
               mu.occurred_end, mu.mentioned_at, mu.fact_type, mu.document_id,
               mu.chunk_id, mu.tags, mu.metadata, mu.proof_count, ml.weight
        FROM %s ml
        JOIN %s mu ON mu.id = ml.to_unit_id
        WHERE ml.from_unit_id = ANY($1::uuid[])
          AND ml.link_type = 'semantic'
          AND mu.fact_type = $2
          AND mu.bank_id = $5
          AND mu.id != ALL($1::uuid[])
        UNION ALL
        SELECT mu.id, mu.text, mu.context, mu.event_date, mu.occurred_start,
               mu.occurred_end, mu.mentioned_at, mu.fact_type, mu.document_id,
               mu.chunk_id, mu.tags, mu.metadata, mu.proof_count, ml.weight
        FROM %s ml
        JOIN %s mu ON mu.id = ml.from_unit_id
        WHERE ml.to_unit_id = ANY($1::uuid[])
          AND ml.link_type = 'semantic'
          AND mu.fact_type = $2
          AND mu.bank_id = $5
          AND mu.id != ALL($1::uuid[])
    ) sem_raw
    GROUP BY id, text, context, event_date, occurred_start, occurred_end,
             mentioned_at, fact_type, document_id, chunk_id, tags, metadata, proof_count
    ORDER BY score DESC
    LIMIT $3
)`, ml, mu, ml, mu)

	causalCTE := fmt.Sprintf(`
causal_expanded AS (
    SELECT DISTINCT ON (mu.id)
        mu.id, mu.text, mu.context, mu.event_date, mu.occurred_start,
        mu.occurred_end, mu.mentioned_at, mu.fact_type, mu.document_id,
        mu.chunk_id, mu.tags, mu.metadata, mu.proof_count,
        ml.weight AS score,
        'causal'::text AS source
    FROM %s ml
    JOIN %s mu ON ml.to_unit_id = mu.id
    WHERE ml.from_unit_id = ANY($1::uuid[])
      AND ml.link_type IN ('causes', 'caused_by', 'enables', 'prevents')
      AND ml.weight >= $4
      AND mu.fact_type = $2
      AND mu.bank_id = $5
    ORDER BY mu.id, ml.weight DESC
    LIMIT $3
)`, ml, mu)

	query := fmt.Sprintf(`
WITH %s,
%s,
%s
SELECT * FROM entity_expanded
UNION ALL
SELECT * FROM semantic_expanded
UNION ALL
SELECT * FROM causal_expanded
`, entityCTE, semanticCTE, causalCTE)

	params := []any{seedUnitIDs, factType, limit, causalThreshold, bankID}

	rows, err := q.Query(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("graph query: %w", err)
	}
	defer rows.Close()

	entityScores := make(map[string]float64)
	semanticScores := make(map[string]float64)
	causalScores := make(map[string]float64)
	rowMap := make(map[string]*FullRetrievalResult)

	for rows.Next() {
		var r FullRetrievalResult
		var score float64
		var source string
		var eventDate, occurredStart, occurredEnd, mentionedAt *time.Time
		var docID, chunkID, contextStr *string
		var tagsArr []string
		var metadataBytes []byte

		err := rows.Scan(
			&r.ID, &r.Text, &contextStr, &eventDate, &occurredStart, &occurredEnd, &mentionedAt,
			&r.FactType, &docID, &chunkID, &tagsArr, &metadataBytes, &r.ProofCount,
			&score, &source,
		)
		if err != nil {
			return nil, fmt.Errorf("scan graph row: %w", err)
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

		fid := r.ID
		switch source {
		case "entity":
			entityScores[fid] = math.Tanh(score * 0.5)
		case "semantic":
			if score > semanticScores[fid] {
				semanticScores[fid] = score
			}
		case "causal":
			if score > causalScores[fid] {
				causalScores[fid] = score
			}
		}
		if _, ok := rowMap[fid]; !ok {
			rowMap[fid] = &r
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph rows: %w", err)
	}

	// Merge scores: entity + semantic + causal ∈ [0, 3].
	allIDs := make(map[string]struct{})
	for id := range entityScores {
		allIDs[id] = struct{}{}
	}
	for id := range semanticScores {
		allIDs[id] = struct{}{}
	}
	for id := range causalScores {
		allIDs[id] = struct{}{}
	}

	var results []*FullRetrievalResult
	for id := range allIDs {
		r := rowMap[id]
		if r == nil {
			continue
		}
		r.Activation = entityScores[id] + semanticScores[id] + causalScores[id]
		results = append(results, r)
	}

	// Apply tag filtering post-retrieval (same as Python).
	results = filterResultsByTags(results, tags, tagsMatch)

	return results, nil
}

// ---------------------------------------------------------------------------
// Post-retrieval tag filter (used when SQL filtering isn't possible)
// ---------------------------------------------------------------------------

func filterResultsByTags(results []*FullRetrievalResult, tags []string, match TagsMatchMode) []*FullRetrievalResult {
	if len(tags) == 0 {
		return results
	}

	_, includeUntagged := parseTagsMatch(match)
	isAnyMatch := match == TagsMatchAny || match == TagsMatchAnyStrict

	tagsSet := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		tagsSet[t] = struct{}{}
	}

	var filtered []*FullRetrievalResult
	for _, r := range results {
		isUntagged := len(r.Tags) == 0

		if isUntagged {
			if includeUntagged {
				filtered = append(filtered, r)
			}
			continue
		}

		resultTagsSet := make(map[string]struct{}, len(r.Tags))
		for _, t := range r.Tags {
			resultTagsSet[t] = struct{}{}
		}

		if isAnyMatch {
			// Any overlap
			found := false
			for t := range tagsSet {
				if _, ok := resultTagsSet[t]; ok {
					found = true
					break
				}
			}
			if found {
				filtered = append(filtered, r)
			}
		} else {
			// All tags must be present
			allPresent := true
			for t := range tagsSet {
				if _, ok := resultTagsSet[t]; !ok {
					allPresent = false
					break
				}
			}
			if allPresent {
				filtered = append(filtered, r)
			}
		}
	}

	return filtered
}

func parseTagsMatch(match TagsMatchMode) (string, bool) {
	switch match {
	case TagsMatchAll:
		return "@>", true
	case TagsMatchAllStrict:
		return "@>", false
	case TagsMatchAnyStrict:
		return "&&", false
	default:
		return "&&", true
	}
}
