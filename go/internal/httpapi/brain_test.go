package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

func TestUpsertBrainPage(t *testing.T) {
	now := time.Now().UTC()
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema: "public",
			FeatureFlags:   map[string]bool{"banks": true, "memories": true},
		},
		Store: fakeStore{brainPage: &store.BrainPage{
			Slug:       "daily-note",
			Title:      "Daily Note",
			Type:       "note",
			Content:    "compiled truth",
			DocumentID: "brain:daily-note",
			ChunkID:    "brain:user123:daily-note:0",
			MemoryID:   "00000000-0000-0000-0000-000000000001",
			CreatedAt:  now,
			UpdatedAt:  now,
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/brain/pages", strings.NewReader(`{
		"slug":"daily-note",
		"title":"Daily Note",
		"content":"compiled truth"
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body store.BrainPage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Slug != "daily-note" || body.DocumentID != "brain:daily-note" {
		t.Fatalf("unexpected brain page response: %+v", body)
	}
}

func TestListBrainPages(t *testing.T) {
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema: "public",
			FeatureFlags:   map[string]bool{"banks": true, "memories": true},
		},
		Store: fakeStore{brainPages: []store.BrainPage{{Slug: "a", Title: "A"}}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/default/banks/user123/brain/pages?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"slug":"a"`) {
		t.Fatalf("expected page list, got %s", rec.Body.String())
	}
}

func TestBrainLinksBacklinksAndTimeline(t *testing.T) {
	now := time.Now().UTC()
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema: "public",
			FeatureFlags:   map[string]bool{"banks": true, "memories": true},
		},
		Store: fakeStore{
			brainLinks: []store.BrainLink{{
				FromSlug:  "agent-memory",
				ToSlug:    "retrieval",
				LinkType:  "mentions",
				Context:   "retrieval depends on memory",
				CreatedAt: now,
			}},
			brainTimelineEntry: &store.BrainTimelineEntry{
				ID:        "00000000-0000-0000-0000-000000000002",
				Slug:      "agent-memory",
				Date:      "2026-04-30",
				Source:    "import",
				Summary:   "Imported page history",
				CreatedAt: now,
			},
			brainTimeline: []store.BrainTimelineEntry{{
				ID:      "00000000-0000-0000-0000-000000000002",
				Slug:    "agent-memory",
				Date:    "2026-04-30",
				Summary: "Imported page history",
			}},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/brain/links", strings.NewReader(`{
		"from_slug":"agent-memory",
		"to_slug":"retrieval",
		"link_type":"mentions"
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected link write 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/default/banks/user123/brain/pages/agent-memory/backlinks", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"to_slug":"retrieval"`) {
		t.Fatalf("expected backlink response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/brain/pages/agent-memory/timeline", strings.NewReader(`{
		"date":"2026-04-30",
		"source":"import",
		"summary":"Imported page history"
	}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"summary":"Imported page history"`) {
		t.Fatalf("expected timeline write response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/default/banks/user123/brain/pages/agent-memory/timeline", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"timeline"`) {
		t.Fatalf("expected timeline list response, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBrainJobQueueHandlers(t *testing.T) {
	now := time.Now().UTC()
	job := &store.BrainJob{
		ID:        "00000000-0000-0000-0000-000000000003",
		BankID:    "user123",
		Kind:      "brain.sync",
		Status:    "queued",
		Priority:  10,
		Params:    map[string]any{"source_id": "import"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema: "public",
			FeatureFlags:   map[string]bool{"banks": true, "memories": true},
		},
		Store: fakeStore{
			brainJob:  job,
			brainJobs: []store.BrainJob{*job},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/brain/jobs", strings.NewReader(`{
		"kind":"brain.sync",
		"priority":10,
		"params":{"source_id":"import"}
	}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"kind":"brain.sync"`) {
		t.Fatalf("expected enqueue response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/brain/jobs/claim", strings.NewReader(`{"kinds":["brain.sync"]}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"job"`) {
		t.Fatalf("expected claim response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/default/banks/user123/brain/jobs/00000000-0000-0000-0000-000000000003/complete", strings.NewReader(`{
		"status":"succeeded",
		"result":{"imported":1}
	}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"00000000-0000-0000-0000-000000000003"`) {
		t.Fatalf("expected complete response, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/default/banks/user123/brain/jobs?status=queued", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"jobs"`) {
		t.Fatalf("expected jobs response, got %d body=%s", rec.Code, rec.Body.String())
	}
}
