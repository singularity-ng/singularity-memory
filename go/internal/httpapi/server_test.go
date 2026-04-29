package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/storageprofile"
	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

type fakeStore struct {
	pingErr error
	banks   []store.BankListItem
}

func (f fakeStore) Ping(context.Context) error {
	return f.pingErr
}

func (f fakeStore) ListBanks(context.Context) ([]store.BankListItem, error) {
	return f.banks, nil
}

func TestHealthzWithoutStoreIsUnavailable(t *testing.T) {
	handler := NewServer(Dependencies{Config: config.Config{DatabaseSchema: "public"}})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHealthzReportsStorageProfile(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema: "public",
			StorageProfile: storageprofile.PGVECTOR,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["storage_profile"] != "pgvector" {
		t.Fatalf("expected storage_profile pgvector, got %v", body["storage_profile"])
	}
	if body["embed_configured"] != false {
		t.Fatalf("expected embed_configured false, got %v", body["embed_configured"])
	}
	if body["rerank_configured"] != false {
		t.Fatalf("expected rerank_configured false, got %v", body["rerank_configured"])
	}
}

func TestHealthzWithClientsConfigured(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:   "public",
			StorageProfile:   storageprofile.VCHORD,
			EmbedGatewayURL:  "http://embed:8080",
			RerankGatewayURL: "http://rerank:8080",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["storage_profile"] != "vchord" {
		t.Fatalf("expected storage_profile vchord, got %v", body["storage_profile"])
	}
	if body["embed_configured"] != true {
		t.Fatalf("expected embed_configured true, got %v", body["embed_configured"])
	}
	if body["rerank_configured"] != true {
		t.Fatalf("expected rerank_configured true, got %v", body["rerank_configured"])
	}
}

func TestListBanksReturnsCompatibilityEnvelope(t *testing.T) {
	disposition := map[string]int{"skepticism": 3, "literalism": 3, "empathy": 3}
	name := "Alice"
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store: fakeStore{
			banks: []store.BankListItem{{
				BankID:      "user123",
				Name:        &name,
				Disposition: disposition,
				Mission:     "Ship reliable software",
				CreatedAt:   nil,
				UpdatedAt:   nil,
			}},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/default/banks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Banks []store.BankListItem `json:"banks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Banks) != 1 || body.Banks[0].BankID != "user123" {
		t.Fatalf("unexpected banks response: %+v", body)
	}
}

func TestListBanksUnscopedAlias(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store:  fakeStore{banks: []store.BankListItem{}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/banks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
