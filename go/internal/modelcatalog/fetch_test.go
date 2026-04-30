package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchLiveProviderUsesBearerKeyAndParsesModels(t *testing.T) {
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3:8b","name":"Qwen3 8B"},{"id":"glm-4.7"}]}`))
	}))
	defer server.Close()

	catalog := Catalog{GeneratedAt: time.Now().UTC()}
	fetcher := Fetcher{Client: server.Client()}
	fetcher.fetchLiveProvider(context.Background(), &catalog, LiveProvider{
		ID:        "test-provider",
		Name:      "Test Provider",
		BaseURL:   server.URL + "/v1",
		SecretRef: "sf.providers.test-provider.api_key",
		APIKey:    "test-key",
		KeySource: "sf-sops",
	})

	if sawAuth != "Bearer test-key" {
		t.Fatalf("Authorization header = %q", sawAuth)
	}
	if len(catalog.Sources) != 1 || !catalog.Sources[0].OK {
		t.Fatalf("unexpected source status: %+v", catalog.Sources)
	}
	if catalog.Sources[0].AuthRef != "sf.providers.test-provider.api_key" || catalog.Sources[0].AuthSource != "sf-sops" || !catalog.Sources[0].AuthPresent {
		t.Fatalf("unexpected auth status: %+v", catalog.Sources[0])
	}
	if len(catalog.Providers) != 1 || catalog.Providers[0].ID != "test-provider" {
		t.Fatalf("unexpected providers: %+v", catalog.Providers)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("model count = %d", len(catalog.Models))
	}
	if catalog.Models[0].CanonicalSlug != "test-provider/qwen3-8b" || catalog.Models[0].SizeClass != "tiny" {
		t.Fatalf("unexpected first model identity: %+v", catalog.Models[0])
	}
}

func TestFetchLiveProviderReportsMissingConfiguredKey(t *testing.T) {
	catalog := Catalog{GeneratedAt: time.Now().UTC()}
	Fetcher{}.fetchLiveProvider(context.Background(), &catalog, LiveProvider{
		ID:         "test-provider",
		BaseURL:    "https://example.test/v1",
		SecretRef:  "sf.providers.test-provider.api_key",
		KeySource:  "sf-sops",
		SecretHint: "sf namespace unavailable",
	})

	if len(catalog.Sources) != 1 {
		t.Fatalf("source count = %d", len(catalog.Sources))
	}
	source := catalog.Sources[0]
	if source.OK || source.AuthPresent || source.Error == "" {
		t.Fatalf("unexpected source status: %+v", source)
	}
	if len(catalog.Models) != 0 {
		t.Fatalf("expected no models, got %+v", catalog.Models)
	}
}

func TestFetchLiveProviderPreservesOllamaTagsEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"kimi-k2.6:cloud"},{"model":"glm-5:cloud"}]}`))
	}))
	defer server.Close()

	catalog := Catalog{GeneratedAt: time.Now().UTC()}
	fetcher := Fetcher{Client: server.Client()}
	fetcher.fetchLiveProvider(context.Background(), &catalog, LiveProvider{
		ID:      "ollama-cloud",
		BaseURL: server.URL + "/api/tags",
	})

	if len(catalog.Sources) != 1 || !catalog.Sources[0].OK {
		t.Fatalf("unexpected source: %+v", catalog.Sources)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("model count = %d", len(catalog.Models))
	}
	if catalog.Models[0].ID != "kimi-k2.6:cloud" || catalog.Models[1].ID != "glm-5:cloud" {
		t.Fatalf("unexpected models: %+v", catalog.Models)
	}
}
