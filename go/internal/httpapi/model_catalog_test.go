package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/modelcatalog"
)

func TestModelCatalogExportStartsEmpty(t *testing.T) {
	service := modelcatalog.NewService("", modelcatalog.Fetcher{})
	handler := NewServer(Dependencies{
		Config:       config.Config{FeatureFlags: map[string]bool{}},
		ModelCatalog: service,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/model-catalog/export/sf", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body modelcatalog.SFExport
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Policy.JudgmentMinimumSizeClass != "standard" {
		t.Fatalf("judgment floor = %q", body.Policy.JudgmentMinimumSizeClass)
	}
}
