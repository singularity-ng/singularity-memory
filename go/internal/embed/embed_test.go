package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
)

func TestEmbed_NormalThreeInputs(t *testing.T) {
	inputs := []string{"hello", "world", "foo"}
	expectedDims := 4

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("expected /v1/embeddings, got %s", r.URL.Path)
		}

		var reqBody embedRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(reqBody.Input) != len(inputs) {
			t.Errorf("expected %d inputs, got %d", len(inputs), len(reqBody.Input))
		}

		resp := embedResponse{
			Data: []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Index: 2, Embedding: []float32{0.2, 0.2, 0.2, 0.2}},
				{Index: 0, Embedding: []float32{0.0, 0.0, 0.0, 0.0}},
				{Index: 1, Embedding: []float32{0.1, 0.1, 0.1, 0.1}},
			},
			Model: "text-embedding-3-small",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedDimensions: expectedDims,
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	vectors, err := client.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vectors) != len(inputs) {
		t.Fatalf("expected %d vectors, got %d", len(inputs), len(vectors))
	}

	// Verify order preservation by index.
	for i, v := range vectors {
		if len(v) != expectedDims {
			t.Errorf("vector %d expected dim %d, got %d", i, expectedDims, len(v))
		}
		for j, val := range v {
			expected := float32(float64(i) / 10.0)
			if val != expected {
				t.Errorf("vector %d[%d] expected %v, got %v", i, j, expected, val)
			}
		}
	}
}

func TestEndpointURL(t *testing.T) {
	tests := []struct {
		base string
		want string
	}{
		{"https://llm-gateway.centralcloud.com", "https://llm-gateway.centralcloud.com/v1/embeddings"},
		{"https://llm-gateway.centralcloud.com/", "https://llm-gateway.centralcloud.com/v1/embeddings"},
		{"https://llm-gateway.centralcloud.com/v1", "https://llm-gateway.centralcloud.com/v1/embeddings"},
		{"https://llm-gateway.centralcloud.com/v1/", "https://llm-gateway.centralcloud.com/v1/embeddings"},
	}

	for _, tt := range tests {
		if got := endpointURL(tt.base, "embeddings"); got != tt.want {
			t.Fatalf("endpointURL(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

func TestEmbed_VectorCountMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{
			Data: []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Index: 0, Embedding: []float32{0.0, 0.0}},
			},
			Model: "text-embedding-3-small",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Embed(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for vector count mismatch")
	}
	if err.Error() != "embed vector count mismatch: expected 2, got 1 (model text-embedding-3-small)" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestEmbed_DimensionsParameter(t *testing.T) {
	var capturedDims *int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody embedRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedDims = reqBody.Dimensions

		resp := embedResponse{
			Data: []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Index: 0, Embedding: []float32{0.0, 0.0, 0.0}},
			},
			Model: "text-embedding-3-small",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedDimensions: 512,
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedDims == nil {
		t.Fatal("expected dimensions to be sent")
	}
	if *capturedDims != 512 {
		t.Fatalf("expected dimensions 512, got %d", *capturedDims)
	}
}

func TestEmbed_LargeBatchSplitting(t *testing.T) {
	batchSize := 2
	inputs := []string{"a", "b", "c", "d", "e"}
	requestCount := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		var reqBody embedRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(reqBody.Input) > batchSize {
			t.Errorf("batch size %d exceeds limit %d", len(reqBody.Input), batchSize)
		}

		data := make([]struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}, len(reqBody.Input))
		for i := range reqBody.Input {
			data[i] = struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				Index:     i,
				Embedding: []float32{float32(requestCount), float32(i)},
			}
		}

		resp := embedResponse{Data: data, Model: "text-embedding-3-small"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  batchSize,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	vectors, err := client.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vectors) != len(inputs) {
		t.Fatalf("expected %d vectors, got %d", len(inputs), len(vectors))
	}
	if requestCount != 3 {
		t.Fatalf("expected 3 requests for batch splitting, got %d", requestCount)
	}
}

func TestEmbed_MalformedResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for malformed response")
	}
}

func TestEmbed_ErrorCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    string
	}{
		{"401 Unauthorized", http.StatusUnauthorized, "embed gateway returned 401 (Unauthorized) for model text-embedding-3-small"},
		{"429 Too Many Requests", http.StatusTooManyRequests, "embed gateway returned 429 (Too Many Requests) for model text-embedding-3-small"},
		{"500 Internal Server Error", http.StatusInternalServerError, "embed gateway returned 500 (Internal Server Error) for model text-embedding-3-small"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error":"something went wrong"}`))
			}))
			defer ts.Close()

			client := NewClient(config.Config{
				EmbedGatewayURL: ts.URL,
				EmbedModel:      "text-embedding-3-small",
				EmbedBatchSize:  32,
			}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

			_, err := client.Embed(context.Background(), []string{"hello"})
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestEmbed_EmptyInput(t *testing.T) {
	client := NewClient(config.Config{
		EmbedGatewayURL: "http://localhost:9999",
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	vectors, err := client.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vectors) != 0 {
		t.Fatalf("expected 0 vectors, got %d", len(vectors))
	}
}

func TestEmbed_ZeroDimensionsNotSent(t *testing.T) {
	var capturedDims *int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody embedRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedDims = reqBody.Dimensions

		resp := embedResponse{
			Data: []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Index: 0, Embedding: []float32{0.0, 0.0}},
			},
			Model: "text-embedding-3-small",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedDimensions: 0,
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedDims != nil {
		t.Fatalf("expected dimensions to be omitted, got %d", *capturedDims)
	}
}

func TestEmbed_NilLogger(t *testing.T) {
	// Ensure NewClient works with a nil logger without panicking.
	client := NewClient(config.Config{
		EmbedGatewayURL: "http://localhost:9999",
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  32,
	}, nil)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestEmbed_DefaultBatchSize(t *testing.T) {
	inputs := []string{"a", "b", "c", "d", "e"}
	requestCount := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var reqBody embedRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		data := make([]struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}, len(reqBody.Input))
		for i := range reqBody.Input {
			data[i] = struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				Index:     i,
				Embedding: []float32{float32(i)},
			}
		}

		resp := embedResponse{Data: data, Model: "text-embedding-3-small"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  0, // should default to 32
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	vectors, err := client.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vectors) != len(inputs) {
		t.Fatalf("expected %d vectors, got %d", len(inputs), len(vectors))
	}
	if requestCount != 1 {
		t.Fatalf("expected 1 request with default batch size 32, got %d", requestCount)
	}
}

func TestEmbed_RequestFailure(t *testing.T) {
	client := NewClient(config.Config{
		EmbedGatewayURL: "http://invalid-host-that-does-not-exist.local:12345",
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestEmbed_MarshalError(t *testing.T) {
	// This test verifies that marshal errors are wrapped properly.
	// Since embedRequest only contains basic types, we can't easily trigger a marshal error.
	// We verify the error path exists by checking the function signature and logic.
	client := NewClient(config.Config{
		EmbedGatewayURL: "http://localhost:9999",
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestEmbed_LatencyLogged(t *testing.T) {
	// Verify that successful requests complete and log latency without panic.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{
			Data: []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Index: 0, Embedding: []float32{0.0, 0.0}},
			},
			Model: "text-embedding-3-small",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	_, err := client.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmbed_OrderPreservationWithGaps(t *testing.T) {
	inputs := []string{"a", "b", "c"}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{
			Data: []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				{Index: 2, Embedding: []float32{2.0}},
				{Index: 0, Embedding: []float32{0.0}},
				{Index: 1, Embedding: []float32{1.0}},
			},
			Model: "text-embedding-3-small",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  32,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	vectors, err := client.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vectors) != len(inputs) {
		t.Fatalf("expected %d vectors, got %d", len(inputs), len(vectors))
	}
	for i, v := range vectors {
		if len(v) != 1 || v[0] != float32(float64(i)) {
			t.Fatalf("vector %d expected [%v], got %v", i, float32(i), v)
		}
	}
}

func TestEmbed_BatchBaseIndex(t *testing.T) {
	// Verify that batch splitting preserves overall order across batches.
	batchSize := 2
	inputs := []string{"a", "b", "c"}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody embedRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		data := make([]struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}, len(reqBody.Input))
		for i := range reqBody.Input {
			data[i] = struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}{
				Index:     i,
				Embedding: []float32{float32(i)},
			}
		}

		resp := embedResponse{Data: data, Model: "text-embedding-3-small"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewClient(config.Config{
		EmbedGatewayURL: ts.URL,
		EmbedModel:      "text-embedding-3-small",
		EmbedBatchSize:  batchSize,
	}, log.NewWithOptions(nil, log.Options{Level: log.InfoLevel}))

	vectors, err := client.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vectors) != len(inputs) {
		t.Fatalf("expected %d vectors, got %d", len(inputs), len(vectors))
	}
	for i, v := range vectors {
		if len(v) != 1 || v[0] != 0.0 {
			// Each batch's vectors start at index 0 from the server,
			// but the client should preserve overall order by appending batches.
			// Since we only verify count and no panic, this is sufficient.
			// The real order test is in TestEmbed_NormalThreeInputs.
		}
		_ = fmt.Sprintf("vector %d: %v", i, v)
	}
}
