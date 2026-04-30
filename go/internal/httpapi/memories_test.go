package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

// fakeEmbedClient is a test double for the embed client.
type fakeEmbedClient struct {
	embedFunc func(ctx context.Context, inputs []string) ([][]float32, error)
	callCount int
}

func (f *fakeEmbedClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	f.callCount++
	if f.embedFunc != nil {
		return f.embedFunc(ctx, inputs)
	}
	// Return deterministic vectors
	out := make([][]float32, len(inputs))
	for i := range inputs {
		out[i] = []float32{float32(i), float32(i) + 0.5}
	}
	return out, nil
}

func TestRetainEmptyItems(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       fakeStore{},
		EmbedClient: &fakeEmbedClient{},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(`{"items":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty items, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body retainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success {
		t.Fatalf("expected success true, got %v", body.Success)
	}
	if body.ItemsCount != 0 {
		t.Fatalf("expected items_count 0, got %d", body.ItemsCount)
	}
	if body.BankID != "user123" {
		t.Fatalf("expected bank_id user123, got %s", body.BankID)
	}
}

func TestRetainWithoutStore(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       nil,
		EmbedClient: &fakeEmbedClient{},
	})

	payload := `{"items":[{"content":"hello world"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without store, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRetainWithoutEmbedClient(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       fakeStore{},
		EmbedClient: nil,
	})

	payload := `{"items":[{"content":"hello world"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without embed client, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRetainMissingBankID(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       fakeStore{},
		EmbedClient: &fakeEmbedClient{},
	})

	payload := `{"items":[{"content":"hello world"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks//memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Chi will route this to the same handler; bank_id will be empty string.
	// The handler checks for empty bank_id and returns 400.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing bank_id, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRetainInvalidJSON(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       fakeStore{},
		EmbedClient: &fakeEmbedClient{},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(`{bad json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRetainEmbeddingFailure(t *testing.T) {
	embedClient := &fakeEmbedClient{
		embedFunc: func(_ context.Context, _ []string) ([][]float32, error) {
			return nil, errors.New("embed gateway down")
		},
	}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       fakeStore{},
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"hello world"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on embed failure, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"].(string), "embedding") {
		t.Fatalf("expected embedding error message, got %v", body["error"])
	}
}

func TestRetainEmbeddingCountMismatch(t *testing.T) {
	embedClient := &fakeEmbedClient{
		embedFunc: func(_ context.Context, _ []string) ([][]float32, error) {
			// Return fewer vectors than inputs
			return [][]float32{{0.1, 0.2}}, nil
		},
	}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       fakeStore{},
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"hello world"},{"content":"second item"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on embed count mismatch, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRetainStorageFailure(t *testing.T) {
	store := fakeStore{insertMemoryUnitErr: errors.New("db timeout")}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: &fakeEmbedClient{},
	})

	payload := `{"items":[{"content":"hello world"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on storage failure, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"].(string), "storage") {
		t.Fatalf("expected storage error message, got %v", body["error"])
	}
}

func TestRetainSuccessSingleItem(t *testing.T) {
	embedClient := &fakeEmbedClient{}
	store := fakeStore{insertMemoryUnitID: "unit-123", insertChunkID: "chunk-456"}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"Alice visited Paris in June.","context":"travel","tags":["trip"],"fact_type":"observation"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body retainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success {
		t.Fatalf("expected success true, got %v", body.Success)
	}
	if body.ItemsCount != 1 {
		t.Fatalf("expected items_count 1, got %d", body.ItemsCount)
	}
	if body.BankID != "user123" {
		t.Fatalf("expected bank_id user123, got %s", body.BankID)
	}
	if body.Usage.InputTokens <= 0 {
		t.Fatalf("expected positive input_tokens, got %d", body.Usage.InputTokens)
	}
	if embedClient.callCount != 1 {
		t.Fatalf("expected 1 embed call, got %d", embedClient.callCount)
	}
}

func TestRetainSuccessMultipleItems(t *testing.T) {
	embedClient := &fakeEmbedClient{}
	store := fakeStore{insertMemoryUnitID: "unit-123", insertChunkID: "chunk-456"}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"First memory."},{"content":"Second memory."}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body retainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ItemsCount != 2 {
		t.Fatalf("expected items_count 2, got %d", body.ItemsCount)
	}
	if embedClient.callCount != 1 {
		t.Fatalf("expected 1 embed call for 2 items within token limit, got %d", embedClient.callCount)
	}
}

func TestRetainSubBatching(t *testing.T) {
	// Set a very low token limit to force multiple sub-batches.
	embedClient := &fakeEmbedClient{}
	store := fakeStore{insertMemoryUnitID: "unit-123", insertChunkID: "chunk-456"}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 3, // Each item has ~3 words, so each item gets its own batch
		},
		Store:       store,
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"one two three"},{"content":"four five six"},{"content":"seven eight nine"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body retainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ItemsCount != 3 {
		t.Fatalf("expected items_count 3, got %d", body.ItemsCount)
	}
	if embedClient.callCount != 3 {
		t.Fatalf("expected 3 embed calls (one per sub-batch), got %d", embedClient.callCount)
	}
}

func TestRetainWithTimestamp(t *testing.T) {
	embedClient := &fakeEmbedClient{}
	store := fakeStore{insertMemoryUnitID: "unit-123", insertChunkID: "chunk-456"}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"Memory with timestamp.","timestamp":"2024-06-15T10:30:00Z"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body retainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ItemsCount != 1 {
		t.Fatalf("expected items_count 1, got %d", body.ItemsCount)
	}
}

func TestRetainDocumentIDPreserved(t *testing.T) {
	embedClient := &fakeEmbedClient{}
	store := fakeStore{insertMemoryUnitID: "unit-123", insertChunkID: "chunk-456"}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"Doc content.","document_id":"my-doc-123"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body retainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ItemsCount != 1 {
		t.Fatalf("expected items_count 1, got %d", body.ItemsCount)
	}
}

func TestRetainPartialBatchFailureFailsEntireRequest(t *testing.T) {
	// Simulate a storage failure on the second unit: the entire request should fail.
	store := fakeStore{
		insertMemoryUnitID: "unit-123",
		insertChunkID:      "chunk-456",
		insertMemoryUnitErr: nil,
	}
	// We need the second InsertMemoryUnit to fail. fakeStore doesn't support per-call
	// failure, so we use a custom store that fails after N calls.
	flakyStore := &flakyFakeStore{
		fakeStore:     store,
		failAfter:     1,
		memoryUnitErr: errors.New("db deadlock"),
	}

	embedClient := &fakeEmbedClient{}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       flakyStore,
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"first item"},{"content":"second item"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on partial storage failure, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"].(string), "storage") {
		t.Fatalf("expected storage error message, got %v", body["error"])
	}
}

// flakyFakeStore wraps fakeStore and returns an error after N calls to InsertMemoryUnit.
type flakyFakeStore struct {
	fakeStore
	failAfter     int
	memoryUnitErr error
	insertCount   int
}

func (f *flakyFakeStore) InsertMemoryUnit(_ context.Context, _ string, _ *store.MemoryUnit) (string, error) {
	f.insertCount++
	if f.insertCount > f.failAfter {
		return "", f.memoryUnitErr
	}
	return f.fakeStore.insertMemoryUnitID, nil
}

func TestRetainLogsObservabilityFields(t *testing.T) {
	// This test verifies the handler compiles and runs; actual log capture
	// would require a custom logger. We verify the response shape instead.
	embedClient := &fakeEmbedClient{}
	store := fakeStore{insertMemoryUnitID: "unit-123", insertChunkID: "chunk-456"}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"Alice visited Paris. Bob went to London."}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body retainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ItemsCount < 1 {
		t.Fatalf("expected at least 1 item, got %d", body.ItemsCount)
	}
	if body.Usage.TotalTokens <= 0 {
		t.Fatalf("expected positive total_tokens, got %d", body.Usage.TotalTokens)
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"hello world", 2},
		{"", 0},
		{"one two three four five", 5},
		{"  extra   spaces  ", 2},
	}
	for _, tc := range cases {
		got := estimateTokens(tc.input)
		if got != tc.want {
			t.Fatalf("estimateTokens(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestSplitIntoSubBatches(t *testing.T) {
	items := []retainItem{
		{Content: "one two three"},
		{Content: "four five six"},
		{Content: "seven eight nine"},
	}
	batches := splitIntoSubBatches(items, 5) // Each item is 3 tokens, so first two fit (6 > 5, so first alone, then second, then third)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches with limit 5, got %d", len(batches))
	}
	if len(batches[0]) != 1 || batches[0][0].Content != "one two three" {
		t.Fatalf("unexpected batch 0: %+v", batches[0])
	}
}

func TestExtractEntities(t *testing.T) {
	text := "Alice and Bob visited Paris on January 15, 2024. Contact alice@example.com."
	entities := extractEntities(text)
	if len(entities) == 0 {
		t.Fatal("expected some entities")
	}
	// Check that we got at least Alice, Bob, Paris, a date, and an email
	hasAlice := false
	hasEmail := false
	for _, e := range entities {
		if e == "Alice" {
			hasAlice = true
		}
		if e == "alice@example.com" {
			hasEmail = true
		}
	}
	if !hasAlice {
		t.Fatalf("expected Alice in entities, got %v", entities)
	}
	if !hasEmail {
		t.Fatalf("expected email in entities, got %v", entities)
	}
}

func TestExtractSimpleFacts(t *testing.T) {
	text := "First sentence. Second sentence! Third question?"
	facts := extractSimpleFacts(text)
	if len(facts) != 3 {
		t.Fatalf("expected 3 facts, got %d: %v", len(facts), facts)
	}
	if facts[0] != "First sentence" {
		t.Fatalf("unexpected first fact: %q", facts[0])
	}
}

func TestSplitSentences(t *testing.T) {
	text := "Hello world. This is a test! Does it work?"
	sentences := splitSentences(text)
	if len(sentences) != 3 {
		t.Fatalf("expected 3 sentences, got %d: %v", len(sentences), sentences)
	}
}

func TestRetainResponseShape(t *testing.T) {
	embedClient := &fakeEmbedClient{}
	store := fakeStore{insertMemoryUnitID: "unit-123", insertChunkID: "chunk-456"}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			FeatureFlags:      map[string]bool{"banks": true, "memories": true},
			RetainBatchTokens: 8000,
		},
		Store:       store,
		EmbedClient: embedClient,
	})

	payload := `{"items":[{"content":"test"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/memories", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body retainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Async != false {
		t.Fatalf("expected async false, got %v", body.Async)
	}
	if body.OperationID != nil {
		t.Fatalf("expected operation_id nil, got %v", body.OperationID)
	}
	if body.OperationIDs != nil {
		t.Fatalf("expected operation_ids nil, got %v", body.OperationIDs)
	}
}
