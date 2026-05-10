package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

type brainPageRequest struct {
	Slug        string         `json:"slug"`
	SourceID    string         `json:"source_id,omitempty"`
	Title       string         `json:"title"`
	Type        string         `json:"type,omitempty"`
	PageKind    string         `json:"page_kind,omitempty"`
	Content     string         `json:"content"`
	Timeline    string         `json:"timeline,omitempty"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Source      *string        `json:"source,omitempty"`
}

type brainLinkRequest struct {
	SourceID       string  `json:"source_id,omitempty"`
	FromSlug       string  `json:"from_slug"`
	ToSlug         string  `json:"to_slug"`
	LinkType       string  `json:"link_type,omitempty"`
	Context        string  `json:"context,omitempty"`
	LinkSource     *string `json:"link_source,omitempty"`
	OriginSlug     *string `json:"origin_slug,omitempty"`
	OriginField    *string `json:"origin_field,omitempty"`
	ResolutionType *string `json:"resolution_type,omitempty"`
}

type brainTimelineRequest struct {
	SourceID string `json:"source_id,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Date     string `json:"date"`
	Source   string `json:"source,omitempty"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail,omitempty"`
}

type brainJobRequest struct {
	Kind     string         `json:"kind"`
	Priority int            `json:"priority,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
}

type brainJobClaimRequest struct {
	Kinds []string `json:"kinds,omitempty"`
}

type brainJobCompleteRequest struct {
	Status string         `json:"status,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Error  *string        `json:"error,omitempty"`
}

func (s *server) upsertBrainPage(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}

	bankID := chi.URLParam(r, "bank_id")
	if strings.TrimSpace(bankID) == "" {
		writeError(w, http.StatusBadRequest, "bank_id is required")
		return
	}

	var req brainPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	page, err := s.deps.Store.UpsertBrainPage(r.Context(), bankID, store.BrainPageInput{
		Slug:        req.Slug,
		SourceID:    req.SourceID,
		Title:       req.Title,
		Type:        req.Type,
		PageKind:    req.PageKind,
		Content:     req.Content,
		Timeline:    req.Timeline,
		Frontmatter: req.Frontmatter,
		Tags:        req.Tags,
		Source:      req.Source,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *server) getBrainPage(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	slug := chi.URLParam(r, "slug")
	page, err := s.deps.Store.GetBrainPage(r.Context(), bankID, slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if page == nil {
		writeError(w, http.StatusNotFound, "brain page not found")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *server) listBrainPages(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = parsed
	}
	pages, err := s.deps.Store.ListBrainPages(r.Context(), bankID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
}

func (s *server) addBrainLink(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	var req brainLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	link := store.BrainLink{
		FromSlug:       req.FromSlug,
		ToSlug:         req.ToSlug,
		LinkType:       req.LinkType,
		Context:        req.Context,
		LinkSource:     req.LinkSource,
		OriginSlug:     req.OriginSlug,
		OriginField:    req.OriginField,
		ResolutionType: req.ResolutionType,
	}
	if err := s.deps.Store.AddBrainLink(r.Context(), bankID, req.SourceID, link); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *server) getBrainLinks(w http.ResponseWriter, r *http.Request) {
	s.getBrainLinksWithDirection(w, r, false)
}

func (s *server) getBrainBacklinks(w http.ResponseWriter, r *http.Request) {
	s.getBrainLinksWithDirection(w, r, true)
}

func (s *server) getBrainLinksWithDirection(w http.ResponseWriter, r *http.Request, backlinks bool) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	links, err := s.deps.Store.GetBrainLinks(
		r.Context(),
		chi.URLParam(r, "bank_id"),
		r.URL.Query().Get("source_id"),
		brainSlugParam(r),
		backlinks,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (s *server) addBrainTimeline(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req brainTimelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	entry, err := s.deps.Store.AddBrainTimelineEntry(r.Context(), chi.URLParam(r, "bank_id"), req.SourceID, store.BrainTimelineEntry{
		Slug:    firstNonEmpty(chi.URLParam(r, "slug"), req.Slug, r.URL.Query().Get("slug")),
		Date:    req.Date,
		Source:  req.Source,
		Summary: req.Summary,
		Detail:  req.Detail,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *server) getBrainTimeline(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = parsed
	}
	entries, err := s.deps.Store.GetBrainTimeline(r.Context(), chi.URLParam(r, "bank_id"), r.URL.Query().Get("source_id"), brainSlugParam(r), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"timeline": entries})
}

func brainSlugParam(r *http.Request) string {
	return firstNonEmpty(chi.URLParam(r, "slug"), r.URL.Query().Get("slug"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *server) enqueueBrainJob(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req brainJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	job, err := s.deps.Store.EnqueueBrainJob(r.Context(), chi.URLParam(r, "bank_id"), store.BrainJobInput{
		Kind:     req.Kind,
		Priority: req.Priority,
		Params:   req.Params,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *server) listBrainJobs(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = parsed
	}
	jobs, err := s.deps.Store.ListBrainJobs(r.Context(), chi.URLParam(r, "bank_id"), r.URL.Query().Get("status"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *server) claimBrainJob(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	req := brainJobClaimRequest{}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	job, err := s.deps.Store.ClaimBrainJob(r.Context(), chi.URLParam(r, "bank_id"), req.Kinds)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if job == nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *server) completeBrainJob(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req brainJobCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	job, err := s.deps.Store.CompleteBrainJob(r.Context(), chi.URLParam(r, "bank_id"), chi.URLParam(r, "job_id"), req.Status, req.Result, req.Error)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}
