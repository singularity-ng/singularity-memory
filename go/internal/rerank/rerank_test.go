package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
)

func TestRerank_NormalRequest(t *testing.T) {
	query := "machine learning"
	docs := []string{"deep learning", "neural networks", "baking bread"}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/rerank" {
			t.Errorf("expected /v1/rerank, got %s", r.URL.Path)
		}

		var reqBody rerankRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if reqBody.Query != query {
			t.Errorf("expected query %q, got %q", query, reqBody.Query)
		}
		if len(reqBody.Documents) != len(docs) {
			t.Errorf("expected %d documents, got %d", len(docs), len(reqBody.Documents))
		}
		if reqBody.Model != "cohere-rerank-v3" {
			t.Errorf("expected model cohere-rerank-v3, got %s", reqBody.Model)
		}

		resp := rerankResponse{
			Results: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 1, RelevanceScore: 0.95},
				{Index: 0, RelevanceScore: 0.85},
				{Index: 2, RelevanceScore: 0.10},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		RerankGatewayURL: ts.URL,
		RerankModel:      "cohere-rerank-v3",
		RerankTopK:       10,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	results, err := client.Rerank(context.Background(), query, docs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != len(docs) {
		t.Fatalf("expected %d results, got %d", len(docs), len(results))
	}

	// Verify order preservation by index.
	for i, r := range results {
		if r.Index != i {
			t.Errorf("result %d expected index %d, got %d", i, i, r.Index)
		}
	}
	if results[0].RelevanceScore != 0.85 {
		t.Errorf("expected score 0.85 for index 0, got %f", results[0].RelevanceScore)
	}
	if results[1].RelevanceScore != 0.95 {
		t.Errorf("expected score 0.95 for index 1, got %f", results[1].RelevanceScore)
	}
	if results[2].RelevanceScore != 0.10 {
		t.Errorf("expected score 0.10 for index 2, got %f", results[2].RelevanceScore)
	}
}

func TestEndpointURL(t *testing.T) {
	tests := []struct {
		base string
		want string
	}{
		{"https://llm-gateway.centralcloud.com", "https://llm-gateway.centralcloud.com/v1/rerank"},
		{"https://llm-gateway.centralcloud.com/", "https://llm-gateway.centralcloud.com/v1/rerank"},
		{"https://llm-gateway.centralcloud.com/v1", "https://llm-gateway.centralcloud.com/v1/rerank"},
		{"https://llm-gateway.centralcloud.com/v1/", "https://llm-gateway.centralcloud.com/v1/rerank"},
	}

	for _, tt := range tests {
		if got := endpointURL(tt.base, "rerank"); got != tt.want {
			t.Fatalf("endpointURL(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

func TestRerank_EmptyDocuments(t *testing.T) {
	client := NewClient(config.Config{
		RerankGatewayURL: "http://localhost:9999",
		RerankModel:      "cohere-rerank-v3",
		RerankTopK:       10,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	results, err := client.Rerank(context.Background(), "query", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestRerank_MalformedResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		RerankGatewayURL: ts.URL,
		RerankModel:      "cohere-rerank-v3",
		RerankTopK:       10,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Rerank(context.Background(), "query", []string{"doc"})
	if err == nil {
		t.Fatal("expected error for malformed response")
	}
}

func TestRerank_ErrorCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    string
	}{
		{"401 Unauthorized", http.StatusUnauthorized, "rerank gateway returned 401 (Unauthorized) for model cohere-rerank-v3"},
		{"429 Too Many Requests", http.StatusTooManyRequests, "rerank gateway returned 429 (Too Many Requests) for model cohere-rerank-v3"},
		{"500 Internal Server Error", http.StatusInternalServerError, "rerank gateway returned 500 (Internal Server Error) for model cohere-rerank-v3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error":"something went wrong"}`))
			}))
			defer ts.Close()

			client := NewClient(config.Config{
				RerankGatewayURL: ts.URL,
				RerankModel:      "cohere-rerank-v3",
				RerankTopK:       10,
			}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

			_, err := client.Rerank(context.Background(), "query", []string{"doc"})
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestRerank_TopNDefault(t *testing.T) {
	var capturedTopN int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody rerankRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedTopN = reqBody.TopN

		resp := rerankResponse{
			Results: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 0, RelevanceScore: 0.5},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		RerankGatewayURL: ts.URL,
		RerankModel:      "cohere-rerank-v3",
		RerankTopK:       0, // should default to 10
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Rerank(context.Background(), "query", []string{"doc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedTopN != 10 {
		t.Fatalf("expected top_n default 10, got %d", capturedTopN)
	}
}

func TestRerank_RequestFailure(t *testing.T) {
	client := NewClient(config.Config{
		RerankGatewayURL: "http://invalid-host-that-does-not-exist.local:12345",
		RerankModel:      "cohere-rerank-v3",
		RerankTopK:       10,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Rerank(context.Background(), "query", []string{"doc"})
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestRerank_NilLogger(t *testing.T) {
	client := NewClient(config.Config{
		RerankGatewayURL: "http://localhost:9999",
		RerankModel:      "cohere-rerank-v3",
		RerankTopK:       10,
	}, nil)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}
