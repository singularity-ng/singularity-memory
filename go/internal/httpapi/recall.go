package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/singularity-ng/singularity-memory/go/internal/retrieve"
)

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type recallRequest struct {
	Query          string          `json:"query"`
	Budget         string          `json:"budget,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Types          []string        `json:"types,omitempty"`
	Tags           []string        `json:"tags,omitempty"`
	TagsMatch      string          `json:"tags_match,omitempty"`
	TagGroups      []any           `json:"tag_groups,omitempty"`
	QueryTimestamp *time.Time      `json:"query_timestamp,omitempty"`
	Include        *includeOptions `json:"include,omitempty"`
	Trace          bool            `json:"trace,omitempty"`
	Rerank         bool            `json:"rerank,omitempty"`
}

type includeOptions struct {
	Chunks      *chunkIncludeOptions       `json:"chunks,omitempty"`
	Entities    *entityIncludeOptions      `json:"entities,omitempty"`
	SourceFacts *sourceFactsIncludeOptions `json:"source_facts,omitempty"`
}

type chunkIncludeOptions struct {
	MaxTokens int `json:"max_tokens,omitempty"`
}

type entityIncludeOptions struct {
	MaxTokens int `json:"max_tokens,omitempty"`
}

type sourceFactsIncludeOptions struct{}

type recallResponse struct {
	Results     []recallResult          `json:"results"`
	Chunks      map[string]chunkData    `json:"chunks,omitempty"`
	Entities    map[string]entityState  `json:"entities,omitempty"`
	SourceFacts map[string]recallResult `json:"source_facts,omitempty"`
	Trace       map[string]any          `json:"trace,omitempty"`
	Usage       tokenUsage              `json:"usage,omitempty"`
	Warnings    []string                `json:"warnings,omitempty"`
}

type recallResult struct {
	ID            string            `json:"id"`
	Text          string            `json:"text"`
	Type          string            `json:"type,omitempty"`
	Context       *string           `json:"context,omitempty"`
	ChunkID       *string           `json:"chunk_id,omitempty"`
	DocumentID    *string           `json:"document_id,omitempty"`
	Entities      []string          `json:"entities,omitempty"`
	MentionedAt   *string           `json:"mentioned_at,omitempty"`
	OccurredStart *string           `json:"occurred_start,omitempty"`
	OccurredEnd   *string           `json:"occurred_end,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	SourceFactIDs []string          `json:"source_fact_ids,omitempty"`
	RerankScore   float64           `json:"rerank_score,omitempty"`
}

type chunkData struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	ChunkIndex int    `json:"chunk_index"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type entityState struct {
	EntityID      string              `json:"entity_id"`
	CanonicalName string              `json:"canonical_name"`
	Observations  []entityObservation `json:"observations"`
}

type entityObservation struct {
	Text        string  `json:"text"`
	MentionedAt *string `json:"mentioned_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Recall handler
// ---------------------------------------------------------------------------

func (s *server) recall(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}

	bankID := chi.URLParam(r, "bank_id")
	if bankID == "" {
		writeError(w, http.StatusBadRequest, "bank_id is required")
		return
	}

	var req recallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	start := time.Now()
	ctx := r.Context()

	// Default fact types: world + experience.
	factTypes := req.Types
	if len(factTypes) == 0 {
		factTypes = []string{"world", "experience"}
	}

	// Budget → limit.
	limit := retrieve.BudgetToLimit(req.Budget)

	// Tag match mode.
	tagsMatch := retrieve.TagsMatchMode(req.TagsMatch)
	if tagsMatch == "" {
		tagsMatch = retrieve.TagsMatchAny
	}

	// Temporal extraction from query text.
	_, _ = retrieve.ExtractTemporalTarget(req.Query, time.Now().UTC())

	// Embed query.
	var queryEmbedding []float32
	embedTokens := estimateTokens(req.Query)
	var warnings []string
	if s.deps.EmbedClient != nil {
		embedStart := time.Now()
		vectors, err := s.deps.EmbedClient.Embed(ctx, []string{req.Query})
		embedLatency := time.Since(embedStart)
		if err != nil {
			s.logRecallError("embedding failed; falling back to BM25-only recall", err)
			warnings = append(warnings, "embedding service unavailable; using BM25-only recall")
		} else if len(vectors) == 0 {
			s.logRecallError("embedding returned no vectors; falling back to BM25-only recall", fmt.Errorf("embedding returned no vectors"))
			warnings = append(warnings, "embedding service returned no vectors; using BM25-only recall")
		} else {
			queryEmbedding = vectors[0]
			s.logRecallInfo("recall embed", "latency_ms", embedLatency.Milliseconds(), "tokens", embedTokens)
		}
	} else {
		warnings = append(warnings, "embedding service not configured; using BM25-only recall")
	}

	schema := func(name string) string {
		return `"` + s.deps.Config.DatabaseSchema + `"."` + name + `"`
	}

	// Execute retrieval lanes.
	var semanticResults, bm25Results, graphResults, temporalResults []*retrieve.FullRetrievalResult
	var err error

	// Always run BM25 if query is non-empty.
	bm25Start := time.Now()
	bm25Results, err = retrieve.BM25Retrieval(ctx, s.deps.Store, schema, bankID, req.Query, factTypes, req.Tags, tagsMatch, limit)
	if err != nil {
		s.logRecallError("bm25 retrieval failed", err)
		writeError(w, http.StatusInternalServerError, "retrieval failed: "+err.Error())
		return
	}
	bm25Latency := time.Since(bm25Start)

	// Semantic, graph, and temporal lanes require an embedding.
	var semLatency, graphLatency, temporalLatency time.Duration
	if len(queryEmbedding) > 0 {
		semStart := time.Now()
		semanticResults, err = retrieve.SemanticRetrieval(ctx, s.deps.Store, schema, bankID, queryEmbedding, factTypes, req.Tags, tagsMatch, limit)
		if err != nil {
			s.logRecallError("semantic retrieval failed", err)
			writeError(w, http.StatusInternalServerError, "retrieval failed: "+err.Error())
			return
		}
		semLatency = time.Since(semStart)

		// Graph retrieval: seed from top semantic results.
		seedIDs := make([]string, 0, min(len(semanticResults), 10))
		for i, r := range semanticResults {
			if i >= 10 {
				break
			}
			seedIDs = append(seedIDs, r.ID)
		}
		graphStart := time.Now()
		graphResults, err = retrieve.GraphRetrieval(ctx, s.deps.Store, schema, bankID, seedIDs, factTypes, req.Tags, tagsMatch, limit, 5)
		if err != nil {
			s.logRecallError("graph retrieval failed", err)
			writeError(w, http.StatusInternalServerError, "retrieval failed: "+err.Error())
			return
		}
		graphLatency = time.Since(graphStart)

		temporalStart := time.Now()
		temporalResults, err = retrieve.TemporalRetrieval(ctx, s.deps.Store, schema, bankID, req.Query, req.QueryTimestamp, factTypes, req.Tags, tagsMatch, limit, 5)
		if err != nil {
			s.logRecallError("temporal retrieval failed", err)
			writeError(w, http.StatusInternalServerError, "retrieval failed: "+err.Error())
			return
		}
		temporalLatency = time.Since(temporalStart)
	}

	// Build RRF inputs.
	var rrfInputs []retrieve.RRFInput
	if len(semanticResults) > 0 {
		rrfInputs = append(rrfInputs, retrieve.RRFInput{
			Lane:    retrieve.LaneSemantic,
			Results: toRRFResults(semanticResults, func(r *retrieve.FullRetrievalResult) float64 { return r.Similarity }),
		})
	}
	if len(bm25Results) > 0 {
		rrfInputs = append(rrfInputs, retrieve.RRFInput{
			Lane:    retrieve.LaneBM25,
			Results: toRRFResults(bm25Results, func(r *retrieve.FullRetrievalResult) float64 { return r.BM25Score }),
		})
	}
	if len(graphResults) > 0 {
		rrfInputs = append(rrfInputs, retrieve.RRFInput{
			Lane:    retrieve.LaneGraph,
			Results: toRRFResults(graphResults, func(r *retrieve.FullRetrievalResult) float64 { return r.Activation }),
		})
	}
	if len(temporalResults) > 0 {
		rrfInputs = append(rrfInputs, retrieve.RRFInput{
			Lane:    retrieve.LaneTemporal,
			Results: toRRFResults(temporalResults, func(r *retrieve.FullRetrievalResult) float64 { return r.TemporalScore }),
		})
	}

	// RRF fusion.
	rrfStart := time.Now()
	merged := retrieve.ReciprocalRankFusion(rrfInputs, retrieve.DefaultRRFConfig())
	rrfLatency := time.Since(rrfStart)

	// Trim to budget limit.
	if len(merged) > limit {
		merged = merged[:limit]
	}

	// Build result lookup from all lanes.
	resultMap := buildResultMap(semanticResults, bm25Results, graphResults, temporalResults)

	// Build response results.
	results := make([]recallResult, 0, len(merged))
	for _, cand := range merged {
		src := resultMap[cand.ID]
		if src == nil {
			continue
		}
		results = append(results, fullResultToRecallResult(src, cand.SourceRanks))
	}

	// Reranking: if requested and client is configured, reorder by relevance score.
	var rerankLatency time.Duration
	var rerankErrStr string
	if req.Rerank && s.deps.RerankClient != nil && len(results) > 0 {
		rerankStart := time.Now()
		docs := make([]string, len(results))
		for i, r := range results {
			docs[i] = r.Text
		}
		scores, err := s.deps.RerankClient.Rerank(ctx, req.Query, docs)
		if err != nil {
			s.logRecallError("rerank failed", err)
			rerankErrStr = err.Error()
		} else if len(scores) == len(results) {
			for i := range results {
				results[i].RerankScore = scores[i].RelevanceScore
			}
			// Reorder results by rerank score descending.
			sort.Slice(results, func(i, j int) bool {
				return results[i].RerankScore > results[j].RerankScore
			})
		}
		rerankLatency = time.Since(rerankStart)
	}

	// Observability logging.
	totalLatency := time.Since(start)
	s.logRecallInfo("recall",
		"bank_id", bankID,
		"query", req.Query,
		"limit", limit,
		"semantic_count", len(semanticResults),
		"bm25_count", len(bm25Results),
		"graph_count", len(graphResults),
		"temporal_count", len(temporalResults),
		"result_count", len(results),
		"rrf_ms", rrfLatency.Milliseconds(),
		"semantic_ms", semLatency.Milliseconds(),
		"bm25_ms", bm25Latency.Milliseconds(),
		"graph_ms", graphLatency.Milliseconds(),
		"temporal_ms", temporalLatency.Milliseconds(),
		"rerank_ms", rerankLatency.Milliseconds(),
		"total_ms", totalLatency.Milliseconds(),
	)

	resp := recallResponse{
		Results: results,
		Usage: tokenUsage{
			InputTokens:  embedTokens,
			OutputTokens: 0,
			TotalTokens:  embedTokens,
		},
		Warnings: uniqueWarnings(warnings),
	}

	if req.Trace {
		traceMap := map[string]any{
			"num_results":  len(results),
			"query":        req.Query,
			"time_seconds": fmt.Sprintf("%.3f", totalLatency.Seconds()),
			"lanes": map[string]any{
				"semantic": len(semanticResults),
				"bm25":     len(bm25Results),
				"graph":    len(graphResults),
				"temporal": len(temporalResults),
			},
			"latencies_ms": map[string]any{
				"semantic": semLatency.Milliseconds(),
				"bm25":     bm25Latency.Milliseconds(),
				"graph":    graphLatency.Milliseconds(),
				"temporal": temporalLatency.Milliseconds(),
				"rrf":      rrfLatency.Milliseconds(),
				"rerank":   rerankLatency.Milliseconds(),
				"total":    totalLatency.Milliseconds(),
			},
		}
		if rerankErrStr != "" {
			traceMap["rerank_error"] = rerankErrStr
		}
		resp.Trace = traceMap
	}

	if req.Include != nil {
		if req.Include.Entities != nil {
			resp.Entities = s.buildIncludedEntities(ctx, bankID, results, req.Include.Entities.MaxTokens)
		}
		if req.Include.Chunks != nil {
			resp.Chunks = s.buildIncludedChunks(ctx, bankID, results, req.Include.Chunks.MaxTokens)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// toRRFResults converts FullRetrievalResult slices to RRF input slices.
func toRRFResults(results []*retrieve.FullRetrievalResult, scoreFn func(*retrieve.FullRetrievalResult) float64) []retrieve.RetrievalResult {
	out := make([]retrieve.RetrievalResult, len(results))
	for i, r := range results {
		out[i] = retrieve.RetrievalResult{ID: r.ID, Score: scoreFn(r)}
	}
	return out
}

// buildResultMap merges all lane results into a lookup by ID, preferring the
// first seen (semantic > bm25 > graph > temporal) for metadata.
func buildResultMap(lanes ...[]*retrieve.FullRetrievalResult) map[string]*retrieve.FullRetrievalResult {
	m := make(map[string]*retrieve.FullRetrievalResult)
	for _, lane := range lanes {
		for _, r := range lane {
			if _, ok := m[r.ID]; !ok {
				m[r.ID] = r
			}
		}
	}
	return m
}

func fullResultToRecallResult(r *retrieve.FullRetrievalResult, sourceRanks map[retrieve.Lane]int) recallResult {
	res := recallResult{
		ID:            r.ID,
		Text:          r.Text,
		Type:          r.FactType,
		Tags:          r.Tags,
		Entities:      append([]string(nil), r.Entities...),
		SourceFactIDs: append([]string(nil), r.SourceFactIDs...),
	}

	if r.Context != nil {
		res.Context = r.Context
	}
	if r.DocumentID != nil {
		res.DocumentID = r.DocumentID
	}
	if r.ChunkID != nil {
		res.ChunkID = r.ChunkID
	}
	if r.MentionedAt != nil {
		s := r.MentionedAt.Format(time.RFC3339)
		res.MentionedAt = &s
	}
	if r.OccurredStart != nil {
		s := r.OccurredStart.Format(time.RFC3339)
		res.OccurredStart = &s
	}
	if r.OccurredEnd != nil {
		s := r.OccurredEnd.Format(time.RFC3339)
		res.OccurredEnd = &s
	}
	if len(r.Metadata) > 0 {
		md := make(map[string]string, len(r.Metadata))
		for k, v := range r.Metadata {
			if s, ok := v.(string); ok {
				md[k] = s
			} else {
				md[k] = fmt.Sprintf("%v", v)
			}
		}
		res.Metadata = md
	}

	// Trace source ranks into metadata when present.
	if len(sourceRanks) > 0 {
		if res.Metadata == nil {
			res.Metadata = map[string]string{}
		}
		for lane, rank := range sourceRanks {
			res.Metadata[fmt.Sprintf("source_rank_%s", lane)] = fmt.Sprintf("%d", rank)
		}
	}

	return res
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *server) buildIncludedChunks(ctx context.Context, bankID string, results []recallResult, maxTokens int) map[string]chunkData {
	chunks := make(map[string]chunkData)
	if s.deps.Store == nil {
		return chunks
	}

	documentIDs := make(map[string]struct{})
	for _, result := range results {
		if result.DocumentID != nil && *result.DocumentID != "" {
			documentIDs[*result.DocumentID] = struct{}{}
		}
	}

	for documentID := range documentIDs {
		rows, err := s.deps.Store.GetChunks(ctx, bankID, documentID)
		if err != nil {
			s.logRecallError("include chunks failed", err)
			continue
		}
		for _, row := range rows {
			text, truncated := truncateWords(row.ChunkText, maxTokens)
			chunks[row.ChunkID] = chunkData{
				ID:         row.ChunkID,
				Text:       text,
				ChunkIndex: row.ChunkIndex,
				Truncated:  truncated,
			}
		}
	}

	return chunks
}

func (s *server) buildIncludedEntities(ctx context.Context, bankID string, results []recallResult, maxTokens int) map[string]entityState {
	entities := make(map[string]entityState)
	if s.deps.Store == nil {
		return entities
	}

	names := make(map[string]struct{})
	for _, result := range results {
		for _, name := range result.Entities {
			if strings.TrimSpace(name) != "" {
				names[name] = struct{}{}
			}
		}
	}

	for name := range names {
		observations, err := s.deps.Store.GetEntityObservations(ctx, bankID, name, 20)
		if err != nil {
			s.logRecallError("include entities failed", err)
			continue
		}

		state := entityState{
			EntityID:      name,
			CanonicalName: name,
			Observations:  make([]entityObservation, 0, len(observations)),
		}
		for _, observation := range observations {
			text, _ := truncateWords(observation.Text, maxTokens)
			mentionedAt := observation.MentionedAt.Format(time.RFC3339)
			state.Observations = append(state.Observations, entityObservation{
				Text:        text,
				MentionedAt: &mentionedAt,
			})
		}
		entities[name] = state
	}

	return entities
}

func truncateWords(text string, maxTokens int) (string, bool) {
	if maxTokens <= 0 {
		return text, false
	}
	words := strings.Fields(text)
	if len(words) <= maxTokens {
		return text, false
	}
	return strings.Join(words[:maxTokens], " "), true
}

func (s *server) logRecallError(message string, err error) {
	if s.deps.Logger != nil {
		s.deps.Logger.Error(message, "error", err)
	}
}

func (s *server) logRecallInfo(message string, args ...any) {
	if s.deps.Logger != nil {
		s.deps.Logger.Info(message, args...)
	}
}
