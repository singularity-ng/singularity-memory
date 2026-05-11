package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/config"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

func TestCoreMemoryHandlers(t *testing.T) {
	now := time.Now().UTC()
	block := &store.CoreMemoryBlock{
		BankID:      "user123",
		BlockName:   "persona",
		Content:     "Prefers direct answers.",
		CharLimit:   2000,
		Description: "Durable user preference block",
		UpdatedAt:   now,
	}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema: "public",
			FeatureFlags:   map[string]bool{"banks": true, "memories": true},
		},
		Store: fakeStore{
			coreBlock:  block,
			coreBlocks: []store.CoreMemoryBlock{*block},
		},
	})

	req := httptest.NewRequest(http.MethodPut, "/v1/default/banks/user123/core-memory/persona", strings.NewReader(`{
		"content":"Prefers direct answers.",
		"char_limit":2000,
		"description":"Durable user preference block"
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"block_name":"persona"`) {
		t.Fatalf("expected core-memory set response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/v1/default/banks/user123/core-memory/persona/append", strings.NewReader(`{"text":"Uses VectorChord BM25."}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"content":"Prefers direct answers."`) {
		t.Fatalf("expected core-memory append response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/v1/default/banks/user123/core-memory/persona/replace", strings.NewReader(`{
		"old_text":"direct",
		"new_text":"concise"
	}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"block_name":"persona"`) {
		t.Fatalf("expected core-memory replace response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/default/banks/user123/core-memory", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"core_memory"`) {
		t.Fatalf("expected core-memory list response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/default/banks/user123/core-memory/persona", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected core-memory delete response, got %d body=%s", rec.Code, rec.Body.String())
	}
	var deleted map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &deleted); err != nil {
		t.Fatal(err)
	}
	if deleted["removed"] != true {
		t.Fatalf("expected removed=true, got %v", deleted)
	}
}

func TestContextPacketRoute(t *testing.T) {
	now := time.Now().UTC()
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema: "public",
			FeatureFlags:   map[string]bool{"banks": true, "memories": true},
		},
		Store: fakeStore{
			coreBlocks: []store.CoreMemoryBlock{{
				BankID:    "user123",
				BlockName: "persona",
				Content:   "Uses VectorChord BM25 and core memory blocks.",
				CharLimit: 2000,
				CreatedAt: now,
				UpdatedAt: now,
			}},
			reflection: &store.Reflection{
				BankID: "user123",
				Observations: []store.MemoryUnit{{
					Text: "Recent memories emphasize operations memory.",
				}},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/context", strings.NewReader(`{"query":"what matters","max_tokens":20}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"core_memory:persona"`) || !strings.Contains(rec.Body.String(), `"reflection"`) {
		t.Fatalf("unexpected context packet body: %s", rec.Body.String())
	}
}
