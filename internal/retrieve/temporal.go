package retrieve

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var isoDateRE = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)

// TemporalRetrieval ranks memories by explicit query date proximity when the
// query names a date, otherwise by recent event/mention time. This is the
// first-cut Go port of the temporal lane; link-spreading can build on the same
// result shape in a later parity pass.
func TemporalRetrieval(
	ctx context.Context,
	q Querier,
	schema func(string) string,
	bankID string,
	queryText string,
	queryTimestamp *time.Time,
	factTypes []string,
	tags []string,
	tagsMatch TagsMatchMode,
	limit int,
	maxIterations int,
) ([]*FullRetrievalResult, error) {
	if len(factTypes) == 0 || limit <= 0 {
		return nil, nil
	}

	ref := time.Now().UTC()
	if queryTimestamp != nil {
		ref = queryTimestamp.UTC()
	}

	target, hasTarget := ExtractTemporalTarget(queryText, ref)
	params := []any{bankID, factTypes, limit}

	timeExpr := "COALESCE(occurred_start, occurred_end, mentioned_at, event_date)"
	scoreExpr := "1.0 / (1.0 + EXTRACT(EPOCH FROM ($4::timestamptz - " + timeExpr + ")) / 86400.0)"
	orderExpr := "temporal_score DESC, " + timeExpr + " DESC"
	whereTime := ""
	if hasTarget {
		params = append(params, target)
		scoreExpr = "1.0 / (1.0 + ABS(EXTRACT(EPOCH FROM (" + timeExpr + " - $4::timestamptz))) / 86400.0)"
		orderExpr = "temporal_score DESC"
	} else {
		params = append(params, ref)
		whereTime = "AND " + timeExpr + " <= $4::timestamptz"
	}

	tagsClause, tagsParam, _ := BuildTagsClause(tags, 5, "mu", tagsMatch)
	if tagsParam != nil {
		params = append(params, tagsParam)
	}

	query := fmt.Sprintf(`
SELECT
    id, text, context, event_date, occurred_start, occurred_end, mentioned_at,
    fact_type, document_id, chunk_id, tags, metadata, proof_count,
    %s AS temporal_score,
    0::float AS temporal_proximity
FROM %s mu
WHERE bank_id = $1
  AND fact_type = ANY($2)
  %s
  %s
ORDER BY %s
LIMIT $3
`, scoreExpr, schema("memory_units"), whereTime, tagsClause, orderExpr)

	rows, err := q.Query(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("temporal retrieval: %w", err)
	}
	defer rows.Close()

	var results []*FullRetrievalResult
	for rows.Next() {
		var r FullRetrievalResult
		var eventDate, occurredStart, occurredEnd, mentionedAt *time.Time
		var docID, chunkID, contextStr *string
		var tagsArr []string
		var metadataBytes []byte

		err := rows.Scan(
			&r.ID, &r.Text, &contextStr, &eventDate, &occurredStart, &occurredEnd, &mentionedAt,
			&r.FactType, &docID, &chunkID, &tagsArr, &metadataBytes, &r.ProofCount,
			&r.TemporalScore, &r.TemporalProximity,
		)
		if err != nil {
			return nil, fmt.Errorf("scan temporal row: %w", err)
		}

		r.Context = contextStr
		r.EventDate = eventDate
		r.OccurredStart = occurredStart
		r.OccurredEnd = occurredEnd
		r.MentionedAt = mentionedAt
		r.DocumentID = docID
		r.ChunkID = chunkID
		r.Tags = tagsArr
		if len(metadataBytes) > 0 {
			_ = json.Unmarshal(metadataBytes, &r.Metadata)
		}
		results = append(results, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("temporal rows: %w", err)
	}

	return results, nil
}

// ExtractTemporalTarget recognizes a small deterministic date subset without
// involving an LLM. It returns UTC midnight for relative dates and ISO dates.
func ExtractTemporalTarget(queryText string, ref time.Time) (time.Time, bool) {
	lower := strings.ToLower(queryText)
	base := time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case strings.Contains(lower, "yesterday"):
		return base.AddDate(0, 0, -1), true
	case strings.Contains(lower, "today"):
		return base, true
	case strings.Contains(lower, "tomorrow"):
		return base.AddDate(0, 0, 1), true
	}

	match := isoDateRE.FindStringSubmatch(queryText)
	if len(match) == 2 {
		if parsed, err := time.ParseInLocation(time.DateOnly, match[1], time.UTC); err == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}
