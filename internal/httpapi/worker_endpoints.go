package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

type postmortemRequest struct {
	Fingerprint   string   `json:"fingerprint"`
	RootCause     string   `json:"root_cause"`
	Resolution    string   `json:"resolution"`
	ResolvedBy    string   `json:"resolved_by"`
	RunbookSlugs  []string `json:"runbook_slugs,omitempty"`
}

type ackPostmortemRequest struct {
	Fingerprint string `json:"fingerprint"`
	AckedBy     string `json:"acked_by,omitempty"`
}

func (s *server) enqueueSleep(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	_, err := s.deps.Store.EnqueueBrainJob(r.Context(), bankID, store.BrainJobInput{
		Kind:   "sleep",
		Params: map[string]any{},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "queued",
		"bank_id": bankID,
	})
}

func (s *server) enqueuePostmortem(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req postmortemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	_, err := s.deps.Store.EnqueueBrainJob(r.Context(), bankID, store.BrainJobInput{
		Kind: "postmortem",
		Params: map[string]any{
			"fingerprint":   req.Fingerprint,
			"root_cause":    req.RootCause,
			"resolution":    req.Resolution,
			"resolved_by":   req.ResolvedBy,
			"runbook_slugs": req.RunbookSlugs,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "queued",
		"bank_id": bankID,
	})
}

func (s *server) ackPostmortem(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req ackPostmortemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Fingerprint == "" {
		writeError(w, http.StatusBadRequest, "fingerprint is required")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	n, err := s.deps.Store.AckPostmortemByFingerprint(r.Context(), bankID, req.Fingerprint, req.AckedBy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"acknowledged": n,
		"fingerprint":  req.Fingerprint,
		"bank_id":      bankID,
	})
}

