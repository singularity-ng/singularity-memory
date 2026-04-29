package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/storageprofile"
	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

type fakeStore struct {
	pingErr           error
	banks             []store.BankListItem
	bank              *store.BankProfile
	bankErr           error
	updateBankResult  *store.BankProfile
	updateBankErr     error
	deleteCount       int
	deleteErr         error
}

func (f fakeStore) Ping(context.Context) error {
	return f.pingErr
}

func (f fakeStore) ListBanks(context.Context) ([]store.BankListItem, error) {
	return f.banks, nil
}

func (f fakeStore) GetBank(_ context.Context, bankID string) (*store.BankProfile, error) {
	if f.bank == nil && f.bankErr == nil {
		bg := ""
		return &store.BankProfile{
			BankID:      bankID,
			Name:        bankID,
			Disposition: map[string]int{"skepticism": 3, "literalism": 3, "empathy": 3},
			Mission:     "",
			Background:  &bg,
		}, nil
	}
	return f.bank, f.bankErr
}

func (f fakeStore) UpdateBank(_ context.Context, _ string, _ *string, _ *string, _ map[string]int) (*store.BankProfile, error) {
	return f.updateBankResult, f.updateBankErr
}

func (f fakeStore) DeleteBank(_ context.Context, _ string) (int, error) {
	return f.deleteCount, f.deleteErr
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

func TestGetBankProfile(t *testing.T) {
	bg := "test mission"
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store: fakeStore{
			bank: &store.BankProfile{
				BankID:      "user123",
				Name:        "Alice",
				Disposition: map[string]int{"skepticism": 3, "literalism": 3, "empathy": 3},
				Mission:     "test mission",
				Background:  &bg,
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/default/banks/user123/profile", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["bank_id"] != "user123" {
		t.Fatalf("expected bank_id user123, got %v", body["bank_id"])
	}
	if body["name"] != "Alice" {
		t.Fatalf("expected name Alice, got %v", body["name"])
	}
	if body["mission"] != "test mission" {
		t.Fatalf("expected mission test mission, got %v", body["mission"])
	}
	if body["background"] != "test mission" {
		t.Fatalf("expected background == mission, got %v", body["background"])
	}
	disp, ok := body["disposition"].(map[string]any)
	if !ok {
		t.Fatalf("expected disposition object, got %T", body["disposition"])
	}
	for _, key := range []string{"skepticism", "literalism", "empathy"} {
		v, ok := disp[key].(float64)
		if !ok {
			t.Fatalf("expected disposition %s as number, got %T", key, disp[key])
		}
		if int(v) != 3 {
			t.Fatalf("expected disposition %s = 3, got %v", key, v)
		}
	}
}

func TestGetBankProfileAutoCreates(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store:  fakeStore{bank: nil, bankErr: nil},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/default/banks/newuser/profile", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["bank_id"] != "newuser" {
		t.Fatalf("expected bank_id newuser, got %v", body["bank_id"])
	}
	disp, ok := body["disposition"].(map[string]any)
	if !ok {
		t.Fatalf("expected disposition object, got %T", body["disposition"])
	}
	for _, key := range []string{"skepticism", "literalism", "empathy"} {
		v, ok := disp[key].(float64)
		if !ok {
			t.Fatalf("expected disposition %s as number, got %T", key, disp[key])
		}
		if int(v) != 3 {
			t.Fatalf("expected default disposition %s = 3, got %v", key, v)
		}
	}
}

func TestUpdateBankPut(t *testing.T) {
	bg := "new mission"
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store: fakeStore{
			updateBankResult: &store.BankProfile{
				BankID:      "user123",
				Name:        "Bob",
				Disposition: map[string]int{"skepticism": 3, "literalism": 3, "empathy": 3},
				Mission:     "new mission",
				Background:  &bg,
			},
		},
	})

	payload := `{"name": "Bob", "mission": "new mission"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/default/banks/user123", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "Bob" {
		t.Fatalf("expected name Bob, got %v", body["name"])
	}
	if body["mission"] != "new mission" {
		t.Fatalf("expected mission new mission, got %v", body["mission"])
	}
}

func TestUpdateBankPatchDisposition(t *testing.T) {
	bg := ""
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store: fakeStore{
			updateBankResult: &store.BankProfile{
				BankID:      "user123",
				Name:        "",
				Disposition: map[string]int{"skepticism": 5, "literalism": 3, "empathy": 3},
				Mission:     "",
				Background:  &bg,
			},
		},
	})

	payload := `{"disposition": {"skepticism": 5}}`
	req := httptest.NewRequest(http.MethodPatch, "/v1/default/banks/user123", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	disp, ok := body["disposition"].(map[string]any)
	if !ok {
		t.Fatalf("expected disposition object, got %T", body["disposition"])
	}
	v, ok := disp["skepticism"].(float64)
	if !ok || int(v) != 5 {
		t.Fatalf("expected skepticism = 5, got %v", disp["skepticism"])
	}
}

func TestUpdateBankEmptyBody(t *testing.T) {
	bg := ""
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store: fakeStore{
			updateBankResult: &store.BankProfile{
				BankID:      "user123",
				Name:        "",
				Disposition: map[string]int{"skepticism": 3, "literalism": 3, "empathy": 3},
				Mission:     "",
				Background:  &bg,
			},
		},
	})

	payload := `{}`
	req := httptest.NewRequest(http.MethodPatch, "/v1/default/banks/user123", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteBank(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store:  fakeStore{deleteCount: 3},
	})

	req := httptest.NewRequest(http.MethodDelete, "/v1/default/banks/user123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != true {
		t.Fatalf("expected success true, got %v", body["success"])
	}
	msg, ok := body["message"].(string)
	if !ok || !strings.Contains(msg, "deleted") {
		t.Fatalf("expected message containing 'deleted', got %v", body["message"])
	}
	count, ok := body["deleted_count"].(float64)
	if !ok || int(count) != 3 {
		t.Fatalf("expected deleted_count 3, got %v", body["deleted_count"])
	}
}

func TestDeleteBankNotFound(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store:  fakeStore{deleteCount: 0},
	})

	req := httptest.NewRequest(http.MethodDelete, "/v1/default/banks/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["success"] != true {
		t.Fatalf("expected success true, got %v", body["success"])
	}
	count, ok := body["deleted_count"].(float64)
	if !ok || int(count) != 0 {
		t.Fatalf("expected deleted_count 0, got %v", body["deleted_count"])
	}
}

func TestBankHandlersWithoutStore(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public"},
		Store:  nil,
	})

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/default/banks/user123/profile", ""},
		{http.MethodPut, "/v1/default/banks/user123", `{"name":"x"}`},
		{http.MethodPatch, "/v1/default/banks/user123", `{"name":"x"}`},
		{http.MethodDelete, "/v1/default/banks/user123", ""},
	}

	for _, ep := range endpoints {
		var bodyReader *strings.Reader
		if ep.body != "" {
			bodyReader = strings.NewReader(ep.body)
		} else {
			bodyReader = strings.NewReader("")
		}
		req := httptest.NewRequest(ep.method, ep.path, bodyReader)
		if ep.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: expected 503, got %d body=%s", ep.method, ep.path, rec.Code, rec.Body.String())
		}
	}
}
