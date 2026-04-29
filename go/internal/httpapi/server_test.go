package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/storageprofile"
	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

type fakeStore struct {
	pingErr          error
	banks            []store.BankListItem
	bank             *store.BankProfile
	bankErr          error
	updateBankResult *store.BankProfile
	updateBankErr    error
	deleteCount      int
	deleteErr        error

	// Memory store stubs
	insertMemoryUnitID  string
	insertMemoryUnitErr error
	getMemoryUnit       *store.MemoryUnit
	getMemoryUnitErr    error
	deleteMemoryUnitErr error
	listMemoryUnits     []store.MemoryUnit
	listMemoryUnitsErr  error
	insertMemoryLinkErr error
	getEntityObs        []store.EntityObservation
	getEntityObsErr     error
	insertChunkID       string
	insertChunkErr      error
	getChunks           []store.Chunk
	getChunksErr        error
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

func (f fakeStore) InsertMemoryUnit(_ context.Context, _ string, _ *store.MemoryUnit) (string, error) {
	return f.insertMemoryUnitID, f.insertMemoryUnitErr
}

func (f fakeStore) GetMemoryUnit(_ context.Context, _ string, _ string) (*store.MemoryUnit, error) {
	return f.getMemoryUnit, f.getMemoryUnitErr
}

func (f fakeStore) DeleteMemoryUnit(_ context.Context, _ string, _ string) error {
	return f.deleteMemoryUnitErr
}

func (f fakeStore) ListMemoryUnits(_ context.Context, _ string, _ int, _ int) ([]store.MemoryUnit, error) {
	return f.listMemoryUnits, f.listMemoryUnitsErr
}

func (f fakeStore) InsertMemoryLink(_ context.Context, _ *store.MemoryLink) error {
	return f.insertMemoryLinkErr
}

func (f fakeStore) GetEntityObservations(_ context.Context, _ string, _ string, _ int) ([]store.EntityObservation, error) {
	return f.getEntityObs, f.getEntityObsErr
}

func (f fakeStore) InsertChunk(_ context.Context, _ string, _ *store.Chunk) (string, error) {
	return f.insertChunkID, f.insertChunkErr
}

func (f fakeStore) GetChunks(_ context.Context, _ string, _ string) ([]store.Chunk, error) {
	return f.getChunks, f.getChunksErr
}

func TestHealthzWithoutStoreIsUnavailable(t *testing.T) {
	handler := NewServer(Dependencies{Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}}})

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
			FeatureFlags:   map[string]bool{"banks": true},
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
			EmbedGatewayURL:  "https://llm-gateway.centralcloud.com/v1",
			RerankGatewayURL: "https://llm-gateway.centralcloud.com/v1",
			FeatureFlags:     map[string]bool{"banks": true},
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
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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
	assertJSONFixture(t, rec.Body.Bytes(), "bank_list_success.json")
}

func TestListBanksUnscopedAlias(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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
	assertJSONFixture(t, rec.Body.Bytes(), "bank_profile_success.json")
}

func TestGetBankProfileAutoCreates(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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
	assertJSONFixture(t, rec.Body.Bytes(), "bank_update_success.json")
}

func TestUpdateBankPatchDisposition(t *testing.T) {
	bg := ""
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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

func TestUpdateBankProfileDisposition(t *testing.T) {
	bg := ""
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
		Store: fakeStore{
			updateBankResult: &store.BankProfile{
				BankID:      "user123",
				Name:        "",
				Disposition: map[string]int{"skepticism": 4, "literalism": 2, "empathy": 5},
				Mission:     "",
				Background:  &bg,
			},
		},
	})

	payload := `{"disposition_skepticism": 4, "disposition_literalism": 2, "disposition_empathy": 5}`
	req := httptest.NewRequest(http.MethodPut, "/v1/default/banks/user123/profile", strings.NewReader(payload))
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
	expected := map[string]int{"skepticism": 4, "literalism": 2, "empathy": 5}
	for key, want := range expected {
		v, ok := disp[key].(float64)
		if !ok || int(v) != want {
			t.Fatalf("expected %s = %d, got %v", key, want, disp[key])
		}
	}
}

func TestUpdateBankEmptyBody(t *testing.T) {
	bg := ""
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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
	assertJSONFixture(t, rec.Body.Bytes(), "bank_delete_success.json")
}

func TestDeleteBankNotFound(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
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
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
		Store:  nil,
	})

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/default/banks/user123/profile", ""},
		{http.MethodPut, "/v1/default/banks/user123/profile", `{"disposition_skepticism":4}`},
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

func TestVersionEndpoint(t *testing.T) {
	handler := NewServer(Dependencies{
		Config:  config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"observations": true, "worker": true}},
		Version: "0.4.0-test",
	})

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["api_version"] != "0.4.0-test" {
		t.Fatalf("expected api_version 0.4.0-test, got %v", body["api_version"])
	}
	features, ok := body["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected features object, got %T", body["features"])
	}
	if features["observations"] != true {
		t.Fatalf("expected observations true, got %v", features["observations"])
	}
	if features["worker"] != true {
		t.Fatalf("expected worker true, got %v", features["worker"])
	}
	if features["bank_config_api"] != false {
		t.Fatalf("expected bank_config_api false, got %v", features["bank_config_api"])
	}
	if features["mcp"] != false {
		t.Fatalf("expected mcp false, got %v", features["mcp"])
	}
	if features["file_upload_api"] != false {
		t.Fatalf("expected file_upload_api false, got %v", features["file_upload_api"])
	}
}

func TestFeatureFlagDisabledReturns404(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": false}},
		Store:  fakeStore{banks: []store.BankListItem{}},
	})

	endpoints := []string{
		"/v1/banks",
		"/v1/default/banks",
		"/v1/default/banks/user123/profile",
		"/v1/default/banks/user123",
	}

	for _, path := range endpoints {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404 when banks flag disabled, got %d", path, rec.Code)
		}
	}
}

func TestFeatureFlagEnabledAllowsAccess(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
		Store:  fakeStore{banks: []store.BankListItem{}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/banks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when banks flag enabled, got %d", rec.Code)
	}
}

func assertJSONFixture(t *testing.T, gotBytes []byte, fixtureName string) {
	t.Helper()

	wantBytes, err := os.ReadFile(filepath.Join("testdata", "fixtures", fixtureName))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixtureName, err)
	}

	var got, want any
	if err := json.Unmarshal(gotBytes, &got); err != nil {
		t.Fatalf("response is invalid JSON: %v", err)
	}
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("fixture %s is invalid JSON: %v", fixtureName, err)
	}

	gotCanonical, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("canonicalize response: %v", err)
	}
	wantCanonical, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("canonicalize fixture %s: %v", fixtureName, err)
	}
	if !bytes.Equal(gotCanonical, wantCanonical) {
		t.Fatalf("fixture %s mismatch\nwant: %s\n got: %s", fixtureName, wantCanonical, gotCanonical)
	}
}
