package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/config"
)

func TestOpenAPIEndpointServesFrozenContract(t *testing.T) {
	committed := readCommittedOpenAPI(t)
	handler := NewServer(Dependencies{
		Config:      config.Config{DatabaseSchema: "public", FeatureFlags: map[string]bool{"banks": true}},
		OpenAPIJSON: committed,
	})

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}

	var want, got map[string]any
	if err := json.Unmarshal(committed, &want); err != nil {
		t.Fatalf("committed openapi is invalid JSON: %v", err)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response openapi is invalid JSON: %v", err)
	}
	if !jsonEqual(want, got) {
		t.Fatalf("served openapi differs from committed contract")
	}
}

func TestLoadOpenAPIJSONFindsRepoRootContract(t *testing.T) {
	b, err := LoadOpenAPIJSON()
	if err != nil {
		t.Fatalf("LoadOpenAPIJSON: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("loaded openapi is invalid JSON: %v", err)
	}
	if doc["openapi"] == "" {
		t.Fatalf("openapi version missing")
	}
}

func readCommittedOpenAPI(t *testing.T) []byte {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "openapi.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read committed openapi: %v", err)
	}
	return b
}

func jsonEqual(a, b map[string]any) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aj) == string(bj)
}
