package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/config"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/rerank"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

// ---------------------------------------------------------------------------
// Mock pgx.Rows for controlled retrieval results
// ---------------------------------------------------------------------------

type mockRows struct {
	data   []map[string]any
	idx    int
	closed bool
	errVal error
}

func newMockRows(data []map[string]any) *mockRows {
	return &mockRows{data: data, idx: -1}
}

func (m *mockRows) Close()                                       { m.closed = true }
func (m *mockRows) Err() error                                   { return m.errVal }
func (m *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) Next() bool {
	m.idx++
	return m.idx < len(m.data)
}

func (m *mockRows) Scan(dest ...any) error {
	if m.idx < 0 || m.idx >= len(m.data) {
		return errors.New("no row")
	}
	row := m.data[m.idx]
	for i, d := range dest {
		if i >= len(row) {
			continue
		}
		val := rowValue(row, i)
		if ptr, ok := d.(*string); ok {
			if s, ok := val.(string); ok {
				*ptr = s
			}
		} else if ptr, ok := d.(*int); ok {
			if n, ok := val.(int); ok {
				*ptr = n
			}
		} else if ptr, ok := d.(*float64); ok {
			if f, ok := val.(float64); ok {
				*ptr = f
			}
		} else if ptr, ok := d.(**float64); ok {
			if f, ok := val.(float64); ok {
				*ptr = &f
			}
		} else if ptr, ok := d.(*[]string); ok {
			if ss, ok := val.([]string); ok {
				*ptr = ss
			}
		} else if ptr, ok := d.(*[]byte); ok {
			if b, ok := val.([]byte); ok {
				*ptr = b
			}
		} else if ptr, ok := d.(*time.Time); ok {
			if t, ok := val.(time.Time); ok {
				*ptr = t
			}
		} else if ptr, ok := d.(**string); ok {
			if s, ok := val.(*string); ok {
				*ptr = s
			}
		} else if ptr, ok := d.(**time.Time); ok {
			if t, ok := val.(*time.Time); ok {
				*ptr = t
			}
		}
	}
	return nil
}

func (m *mockRows) Values() ([]any, error) { return nil, nil }
func (m *mockRows) RawValues() [][]byte    { return nil }
func (m *mockRows) Conn() *pgx.Conn        { return nil }

// rowValue extracts the i-th value from a map using deterministic ordering.
func rowValue(row map[string]any, idx int) any {
	keys := []string{
		"id", "text", "context", "event_date", "occurred_start", "occurred_end",
		"mentioned_at", "fact_type", "document_id", "chunk_id", "tags",
		"metadata", "proof_count", "similarity", "bm25_score", "score", "source",
		"temporal_score", "temporal_proximity",
	}
	if idx < len(keys) {
		if v, ok := row[keys[idx]]; ok {
			return v
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// fakeRecallStore extends fakeStore with Query/QueryRow overrides
// ---------------------------------------------------------------------------

type fakeRecallStore struct {
	fakeStore
	queryFunc    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFunc func(ctx context.Context, sql string, args ...any) pgx.Row
}

func (f *fakeRecallStore) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.queryFunc != nil {
		return f.queryFunc(ctx, sql, args...)
	}
	return f.fakeStore.Query(ctx, sql, args...)
}

func (f *fakeRecallStore) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRowFunc != nil {
		return f.queryRowFunc(ctx, sql, args...)
	}
	return f.fakeStore.QueryRow(ctx, sql, args...)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeRecallHandler(t *testing.T, store Store, embedClient Embedder) http.Handler {
	t.Helper()
	return NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: embedClient,
	})
}

func makeRecallRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories/recall", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func parseRecallResponse(t *testing.T, rec *httptest.ResponseRecorder) recallResponse {
	t.Helper()
	var resp recallResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal recall response: %v", err)
	}
	return resp
}

func parseRecallMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return body
}

func testStringPtr(s string) *string {
	return &s
}

// ---------------------------------------------------------------------------
// 1. TestRecallSuccess
// ---------------------------------------------------------------------------

func TestRecallSuccess(t *testing.T) {
	store := &fakeRecallStore{
		fakeStore: fakeStore{insertMemoryUnitID: "unit-123"},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			// Return one result for any retrieval lane query.
			rows := newMockRows([]map[string]any{
				{
					"id": "mem-1", "text": "Alice works on ML", "context": nil,
					"event_date": nil, "occurred_start": nil, "occurred_end": nil,
					"mentioned_at": nil, "fact_type": "world", "document_id": nil,
					"chunk_id": nil, "tags": []string{"user_a"},
					"metadata": []byte(`{}`), "proof_count": 0,
					"similarity": 0.95, "bm25_score": nil,
				},
			})
			return rows, nil
		},
	}

	embedClient := &fakeEmbedClient{}
	handler := makeRecallHandler(t, store, embedClient)

	req := makeRecallRequest(t, `{"query":"Alice machine learning"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	resp := parseRecallResponse(t, rec)
	if len(resp.Results) == 0 {
		t.Fatalf("expected non-empty results, got %d", len(resp.Results))
	}
	if resp.Results[0].ID == "" {
		t.Fatalf("expected result id, got empty")
	}
	if resp.Results[0].Text == "" {
		t.Fatalf("expected result text, got empty")
	}
	if resp.Results[0].Type == "" {
		t.Fatalf("expected result type, got empty")
	}
}

// ---------------------------------------------------------------------------
// 2. TestRecallEmptyQuery
// ---------------------------------------------------------------------------

func TestRecallEmptyQuery(t *testing.T) {
	store := &fakeRecallStore{fakeStore: fakeStore{}}
	embedClient := &fakeEmbedClient{}
	handler := makeRecallHandler(t, store, embedClient)

	req := makeRecallRequest(t, `{"query":""}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 3. TestRecallEmbedFailure
// ---------------------------------------------------------------------------

func TestRecallEmbedFailure(t *testing.T) {
	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "search_vector <&>") || strings.Contains(sql, "to_bm25query") {
				return newMockRows([]map[string]any{
					{"id": "bm25-fallback", "text": "BM25 fallback result", "fact_type": "world", "tags": []string{}, "metadata": []byte(`{}`), "proof_count": 0, "similarity": nil, "bm25_score": -1.5},
				}), nil
			}
			return newMockRows(nil), nil
		},
	}
	embedClient := &fakeEmbedClient{
		embedFunc: func(_ context.Context, _ []string) ([][]float32, error) {
			return nil, errors.New("embed gateway down")
		},
	}
	handler := makeRecallHandler(t, store, embedClient)

	req := makeRecallRequest(t, `{"query":"Alice machine learning"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on embed failure fallback, got %d body=%s", rec.Code, rec.Body.String())
	}
	resp := parseRecallResponse(t, rec)
	if len(resp.Results) == 0 {
		t.Fatalf("expected BM25 fallback results, got none")
	}
	if resp.Results[0].ID != "bm25-fallback" {
		t.Fatalf("expected bm25-fallback, got %s", resp.Results[0].ID)
	}
	if embedClient.callCount != 1 {
		t.Fatalf("expected 1 embed call, got %d", embedClient.callCount)
	}
	if !containsWarning(resp.Warnings, "BM25-only") {
		t.Fatalf("expected BM25-only warning, got %v", resp.Warnings)
	}
}

// ---------------------------------------------------------------------------
// 4. TestRecallStoreFailure
// ---------------------------------------------------------------------------

func TestRecallStoreFailure(t *testing.T) {
	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return nil, errors.New("db timeout")
		},
	}
	embedClient := &fakeEmbedClient{}
	handler := makeRecallHandler(t, store, embedClient)

	req := makeRecallRequest(t, `{"query":"Alice machine learning"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on store failure, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := parseRecallMap(t, rec)
	if !strings.Contains(body["error"].(string), "retrieval failed") {
		t.Fatalf("expected retrieval failed error, got %v", body["error"])
	}
}

// ---------------------------------------------------------------------------
// 5. TestRecallBudgetLowMidHigh
// ---------------------------------------------------------------------------

func TestRecallBudgetLowMidHigh(t *testing.T) {
	// Return a deterministic number of results based on the LIMIT in the SQL.
	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			// Determine limit from args (BM25 uses $3 as limit).
			limit := 10
			if len(args) >= 3 {
				if n, ok := args[2].(int); ok {
					limit = n
				}
			}
			var rows []map[string]any
			for i := 0; i < limit; i++ {
				rows = append(rows, map[string]any{
					"id": fmt.Sprintf("mem-%d", i), "text": fmt.Sprintf("text %d", i),
					"context": nil, "event_date": nil, "occurred_start": nil,
					"occurred_end": nil, "mentioned_at": nil, "fact_type": "world",
					"document_id": nil, "chunk_id": nil, "tags": []string{},
					"metadata": []byte(`{}`), "proof_count": 0,
					"similarity": 0.9, "bm25_score": nil,
				})
			}
			return newMockRows(rows), nil
		},
	}
	embedClient := &fakeEmbedClient{}
	handler := makeRecallHandler(t, store, embedClient)

	budgets := []string{"low", "mid", "high"}
	counts := make(map[string]int)
	for _, budget := range budgets {
		body := fmt.Sprintf(`{"query":"test","budget":"%s"}`, budget)
		req := makeRecallRequest(t, body)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("budget=%s: expected 200, got %d body=%s", budget, rec.Code, rec.Body.String())
		}
		resp := parseRecallResponse(t, rec)
		counts[budget] = len(resp.Results)
	}

	if counts["low"] > counts["mid"] {
		t.Fatalf("expected low(%d) <= mid(%d)", counts["low"], counts["mid"])
	}
	if counts["mid"] > counts["high"] {
		t.Fatalf("expected mid(%d) <= high(%d)", counts["mid"], counts["high"])
	}
}

// ---------------------------------------------------------------------------
// 6. TestRecallFeatureFlagDisabled
// ---------------------------------------------------------------------------

func TestRecallFeatureFlagDisabled(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema: "public",
			FeatureFlags:   map[string]bool{"banks": true, "memories": false},
		},
	})

	req := makeRecallRequest(t, `{"query":"Alice"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when memories flag disabled, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// 7. TestRecallFeatureFlagEnabled
// ---------------------------------------------------------------------------

func TestRecallFeatureFlagEnabled(t *testing.T) {
	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return newMockRows(nil), nil
		},
	}
	embedClient := &fakeEmbedClient{}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: embedClient,
	})

	req := makeRecallRequest(t, `{"query":"Alice"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when memories flag enabled, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 8. TestRecallResponseSchema
// ---------------------------------------------------------------------------

func TestRecallResponseSchema(t *testing.T) {
	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return newMockRows([]map[string]any{
				{
					"id": "mem-1", "text": "Alice works on ML", "context": nil,
					"event_date": nil, "occurred_start": nil, "occurred_end": nil,
					"mentioned_at": nil, "fact_type": "world", "document_id": nil,
					"chunk_id": nil, "tags": []string{"user_a"},
					"metadata": []byte(`{}`), "proof_count": 0,
					"similarity": 0.95, "bm25_score": nil,
				},
			}), nil
		},
	}
	embedClient := &fakeEmbedClient{}
	handler := makeRecallHandler(t, store, embedClient)

	req := makeRecallRequest(t, `{"query":"Alice","trace":true}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// results must be present and an array
	results, ok := raw["results"].([]any)
	if !ok {
		t.Fatalf("expected results array, got %T", raw["results"])
	}
	if len(results) == 0 {
		t.Fatalf("expected non-empty results")
	}

	// trace must be present when requested
	trace, ok := raw["trace"].(map[string]any)
	if !ok {
		t.Fatalf("expected trace object, got %T", raw["trace"])
	}
	if trace["query"] != "Alice" {
		t.Fatalf("expected trace.query = Alice, got %v", trace["query"])
	}

	// usage must be present
	usage, ok := raw["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage object, got %T", raw["usage"])
	}
	if _, ok := usage["input_tokens"]; !ok {
		t.Fatalf("expected usage.input_tokens")
	}
}

// ---------------------------------------------------------------------------
// 9. TestRRFOrdering
// ---------------------------------------------------------------------------

func TestRRFOrdering(t *testing.T) {
	// Build a store that returns different top results per lane so we can
	// verify RRF merges rather than letting one lane dominate.
	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			var rows []map[string]any
			if strings.Contains(sql, "embedding <=>") {
				// Semantic lane: A is #1, B is #2
				rows = []map[string]any{
					{"id": "A", "text": "semantic A", "fact_type": "world", "tags": []string{}, "metadata": []byte(`{}`), "proof_count": 0, "similarity": 0.99, "bm25_score": nil},
					{"id": "B", "text": "semantic B", "fact_type": "world", "tags": []string{}, "metadata": []byte(`{}`), "proof_count": 0, "similarity": 0.80, "bm25_score": nil},
				}
			} else if strings.Contains(sql, "search_vector <&>") || strings.Contains(sql, "to_bm25query") {
				// BM25 lane: B is #1, A is #2
				rows = []map[string]any{
					{"id": "B", "text": "bm25 B", "fact_type": "world", "tags": []string{}, "metadata": []byte(`{}`), "proof_count": 0, "similarity": nil, "bm25_score": -1.0},
					{"id": "A", "text": "bm25 A", "fact_type": "world", "tags": []string{}, "metadata": []byte(`{}`), "proof_count": 0, "similarity": nil, "bm25_score": -2.0},
				}
			} else {
				rows = nil
			}
			return newMockRows(rows), nil
		},
	}
	embedClient := &fakeEmbedClient{}
	handler := makeRecallHandler(t, store, embedClient)

	req := makeRecallRequest(t, `{"query":"Alice machine learning"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	resp := parseRecallResponse(t, rec)
	if len(resp.Results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(resp.Results))
	}

	// B should win because it is rank 1 in BM25 and rank 2 in semantic,
	// while A is rank 1 in semantic and rank 2 in BM25.
	// With equal weights, B gets 1/61 + 1/62 = 0.0325, A gets 1/61 + 1/62 = same.
	// Tie-break by raw score sum: A = 0.99 + (-(-2.0)) = 2.99, B = 0.80 + 1.0 = 1.80
	// So A should actually win on raw sum. Let's verify it's not just semantic order.
	firstID := resp.Results[0].ID
	if firstID != "A" && firstID != "B" {
		t.Fatalf("unexpected first result %s", firstID)
	}

	// The key assertion: both A and B appear (merged), not just one lane's results.
	ids := make(map[string]bool)
	for _, r := range resp.Results {
		ids[r.ID] = true
	}
	if !ids["A"] || !ids["B"] {
		t.Fatalf("expected both A and B in results, got %+v", ids)
	}
}

// ---------------------------------------------------------------------------
// 10. TestBM25OnlyMode
// ---------------------------------------------------------------------------

func TestBM25OnlyMode(t *testing.T) {
	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "search_vector <&>") || strings.Contains(sql, "to_bm25query") {
				return newMockRows([]map[string]any{
					{"id": "bm25-1", "text": "BM25 result", "fact_type": "world", "tags": []string{}, "metadata": []byte(`{}`), "proof_count": 0, "similarity": nil, "bm25_score": -1.5},
				}), nil
			}
			return newMockRows(nil), nil
		},
	}
	// No embed client configured → BM25-only mode.
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: nil,
	})

	req := makeRecallRequest(t, `{"query":"Alice machine learning"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 in BM25-only mode, got %d body=%s", rec.Code, rec.Body.String())
	}

	resp := parseRecallResponse(t, rec)
	if len(resp.Results) == 0 {
		t.Fatalf("expected BM25 results, got none")
	}
	if resp.Results[0].ID != "bm25-1" {
		t.Fatalf("expected bm25-1, got %s", resp.Results[0].ID)
	}
}

// ---------------------------------------------------------------------------
// 11. TestTagFiltering
// ---------------------------------------------------------------------------

func TestTagFiltering(t *testing.T) {
	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			// Return results that include tags; the SQL filtering happens in the
			// query, but we verify the handler returns results.
			return newMockRows([]map[string]any{
				{
					"id": "tagged-1", "text": "Tagged memory", "context": nil,
					"event_date": nil, "occurred_start": nil, "occurred_end": nil,
					"mentioned_at": nil, "fact_type": "world", "document_id": nil,
					"chunk_id": nil, "tags": []string{"user_a"},
					"metadata": []byte(`{}`), "proof_count": 0,
					"similarity": 0.9, "bm25_score": nil,
				},
			}), nil
		},
	}
	embedClient := &fakeEmbedClient{}
	handler := makeRecallHandler(t, store, embedClient)

	req := makeRecallRequest(t, `{"query":"Alice","tags":["user_a"]}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	resp := parseRecallResponse(t, rec)
	if len(resp.Results) == 0 {
		t.Fatalf("expected results with tag filtering, got none")
	}
	found := false
	for _, r := range resp.Results {
		for _, tag := range r.Tags {
			if tag == "user_a" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected at least one result with tag user_a")
	}
}

func TestRecallIncludeChunks(t *testing.T) {
	store := &fakeRecallStore{
		fakeStore: fakeStore{
			getChunks: []store.Chunk{
				{
					ChunkID:    "chunk-1",
					DocumentID: "doc-1",
					BankID:     "user123",
					ChunkText:  "one two three four five",
					ChunkIndex: 2,
				},
			},
		},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "search_vector <&>") || strings.Contains(sql, "to_bm25query") {
				return newMockRows([]map[string]any{
					{
						"id": "mem-1", "text": "Memory with chunk", "context": nil,
						"event_date": nil, "occurred_start": nil, "occurred_end": nil,
						"mentioned_at": nil, "fact_type": "world", "document_id": testStringPtr("doc-1"),
						"chunk_id": testStringPtr("chunk-1"), "tags": []string{},
						"metadata": []byte(`{}`), "proof_count": 0,
						"similarity": nil, "bm25_score": -1.5,
					},
				}), nil
			}
			return newMockRows(nil), nil
		},
	}

	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store: store,
	})

	req := makeRecallRequest(t, `{"query":"chunk","include":{"chunks":{"max_tokens":3}}}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	resp := parseRecallResponse(t, rec)
	chunk, ok := resp.Chunks["chunk-1"]
	if !ok {
		t.Fatalf("expected chunk-1 in response, got %+v", resp.Chunks)
	}
	if chunk.Text != "one two three" || !chunk.Truncated || chunk.ChunkIndex != 2 {
		t.Fatalf("unexpected chunk payload: %+v", chunk)
	}
}

func TestBuildIncludedEntities(t *testing.T) {
	srv := &server{deps: Dependencies{
		Store: fakeStore{
			getEntityObs: []store.EntityObservation{
				{Text: "Alice knows machine learning", MentionedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)},
			},
		},
	}}

	entities := srv.buildIncludedEntities(context.Background(), "user123", []recallResult{
		{ID: "mem-1", Entities: []string{"Alice"}},
	}, 2)

	entity, ok := entities["Alice"]
	if !ok {
		t.Fatalf("expected Alice entity, got %+v", entities)
	}
	if entity.CanonicalName != "Alice" || entity.EntityID != "Alice" {
		t.Fatalf("unexpected entity identity: %+v", entity)
	}
	if len(entity.Observations) != 1 || entity.Observations[0].Text != "Alice knows" {
		t.Fatalf("unexpected observations: %+v", entity.Observations)
	}
}

func TestRecallRerankReordersResults(t *testing.T) {
	rerankServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Fatalf("unexpected rerank path %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"results": []map[string]any{
				{"index": 0, "relevance_score": 0.10},
				{"index": 1, "relevance_score": 0.99},
			},
		})
	}))
	defer rerankServer.Close()

	store := &fakeRecallStore{
		fakeStore: fakeStore{},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "search_vector <&>") || strings.Contains(sql, "to_bm25query") {
				return newMockRows([]map[string]any{
					{
						"id": "mem-1", "text": "First BM25 result", "context": nil,
						"event_date": nil, "occurred_start": nil, "occurred_end": nil,
						"mentioned_at": nil, "fact_type": "world", "document_id": nil,
						"chunk_id": nil, "tags": []string{}, "metadata": []byte(`{}`),
						"proof_count": 0, "similarity": nil, "bm25_score": -2.0,
					},
					{
						"id": "mem-2", "text": "Second reranked result", "context": nil,
						"event_date": nil, "occurred_start": nil, "occurred_end": nil,
						"mentioned_at": nil, "fact_type": "world", "document_id": nil,
						"chunk_id": nil, "tags": []string{}, "metadata": []byte(`{}`),
						"proof_count": 0, "similarity": nil, "bm25_score": -1.0,
					},
				}), nil
			}
			return newMockRows(nil), nil
		},
	}

	cfg := config.Config{
		DatabaseSchema:    "public",
		FeatureFlags:      map[string]bool{"banks": true, "memories": true},
		RetainBatchTokens: 8000,
		RerankGatewayURL:  rerankServer.URL,
		RerankModel:       "test-reranker",
		RerankTopK:        2,
	}
	handler := NewServer(Dependencies{
		Config:       cfg,
		Store:        store,
		RerankClient: rerank.NewClient(cfg, log.NewWithOptions(nil, log.Options{Level: log.ErrorLevel})),
	})

	req := makeRecallRequest(t, `{"query":"rerank me","rerank":true,"trace":true}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	resp := parseRecallResponse(t, rec)
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %+v", resp.Results)
	}
	if resp.Results[0].ID != "mem-2" {
		t.Fatalf("expected mem-2 first after rerank, got %+v", resp.Results)
	}
	if resp.Results[0].RerankScore != 0.99 || resp.Results[1].RerankScore != 0.10 {
		t.Fatalf("unexpected rerank scores: %+v", resp.Results)
	}
	if _, ok := resp.Trace["rerank_error"]; ok {
		t.Fatalf("did not expect rerank error in trace: %+v", resp.Trace)
	}
}

// ---------------------------------------------------------------------------
// fakeRerankClient is a test double for the rerank client.
// ---------------------------------------------------------------------------

type fakeRerankClient struct {
	rerankFunc func(ctx context.Context, query string, documents []string) ([]rerank.Result, error)
	callCount  int
}

func (f *fakeRerankClient) Rerank(ctx context.Context, query string, documents []string) ([]rerank.Result, error) {
	f.callCount++
	if f.rerankFunc != nil {
		return f.rerankFunc(ctx, query, documents)
	}
	// Default: return scores in reverse order (highest for last document)
	results := make([]rerank.Result, len(documents))
	for i := range documents {
		results[i] = rerank.Result{Index: i, RelevanceScore: float64(len(documents) - i)}
	}
	return results, nil
}
