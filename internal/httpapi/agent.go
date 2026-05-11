package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

type coreMemoryRequest struct {
	Content     string `json:"content"`
	CharLimit   int    `json:"char_limit,omitempty"`
	Description string `json:"description,omitempty"`
}

type coreMemoryAppendRequest struct {
	Text string `json:"text"`
}

type coreMemoryReplaceRequest struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type consolidateRequest struct {
	Limit int `json:"limit,omitempty"`
}

func (s *server) upsertCoreMemory(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req coreMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	block, err := s.deps.Store.UpsertCoreMemoryBlock(r.Context(), store.CoreMemoryBlock{
		BankID:      chi.URLParam(r, "bank_id"),
		BlockName:   chi.URLParam(r, "block_name"),
		Content:     req.Content,
		CharLimit:   req.CharLimit,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, block)
}

func (s *server) appendCoreMemory(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req coreMemoryAppendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	block, err := s.deps.Store.AppendCoreMemoryBlock(
		r.Context(),
		chi.URLParam(r, "bank_id"),
		chi.URLParam(r, "block_name"),
		req.Text,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, block)
}

func (s *server) replaceCoreMemory(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	var req coreMemoryReplaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	block, err := s.deps.Store.ReplaceCoreMemoryBlock(
		r.Context(),
		chi.URLParam(r, "bank_id"),
		chi.URLParam(r, "block_name"),
		req.OldText,
		req.NewText,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, block)
}

func (s *server) deleteCoreMemory(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	removed, err := s.deps.Store.DeleteCoreMemoryBlock(
		r.Context(),
		chi.URLParam(r, "bank_id"),
		chi.URLParam(r, "block_name"),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bank_id":    chi.URLParam(r, "bank_id"),
		"block_name": chi.URLParam(r, "block_name"),
		"removed":    removed,
	})
}

func (s *server) listCoreMemory(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	blocks, err := s.deps.Store.ListCoreMemoryBlocks(r.Context(), chi.URLParam(r, "bank_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"core_memory": blocks})
}

func (s *server) consolidateMemory(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	req := consolidateRequest{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	result, err := s.deps.Store.RunDeterministicConsolidation(r.Context(), chi.URLParam(r, "bank_id"), req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) reflectMemory(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = parsed
	}
	reflection, err := s.deps.Store.ReflectAgentMemory(r.Context(), chi.URLParam(r, "bank_id"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reflection)
}
