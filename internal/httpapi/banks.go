package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

type updateBankRequest struct {
	Name                  *string        `json:"name"`
	Mission               *string        `json:"mission"`
	Disposition           map[string]int `json:"disposition"`
	Background            *string        `json:"background"`
	DispositionEmpathy    *int           `json:"disposition_empathy"`
	DispositionLiteralism *int           `json:"disposition_literalism"`
	DispositionSkepticism *int           `json:"disposition_skepticism"`
}

type deleteResponse struct {
	Success      bool    `json:"success"`
	Message      *string `json:"message"`
	DeletedCount *int    `json:"deleted_count"`
}

type bankProfileResponse struct {
	BankID      string         `json:"bank_id"`
	Name        string         `json:"name"`
	Disposition map[string]int `json:"disposition"`
	Mission     string         `json:"mission"`
	Background  *string        `json:"background"`
}

func bankProfileToResponse(p *store.BankProfile) bankProfileResponse {
	return bankProfileResponse{
		BankID:      p.BankID,
		Name:        p.Name,
		Disposition: p.Disposition,
		Mission:     p.Mission,
		Background:  p.Background,
	}
}

func (s *server) listBanks(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	banks, err := s.deps.Store.ListBanks(r.Context())
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Error("list banks failed", "error", err)
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"banks": banks})
}

func (s *server) getBank(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	profile, err := s.deps.Store.GetBank(r.Context(), bankID)
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Error("get bank failed", "error", err, "bank_id", bankID)
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bankProfileToResponse(profile))
}

func (s *server) updateBank(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")

	var req updateBankRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	disposition := req.Disposition
	if disposition == nil {
		disposition = make(map[string]int)
	}
	if req.DispositionEmpathy != nil {
		disposition["empathy"] = *req.DispositionEmpathy
	}
	if req.DispositionLiteralism != nil {
		disposition["literalism"] = *req.DispositionLiteralism
	}
	if req.DispositionSkepticism != nil {
		disposition["skepticism"] = *req.DispositionSkepticism
	}
	if len(disposition) == 0 {
		disposition = nil
	}

	var name, mission *string
	if req.Name != nil {
		name = req.Name
	}
	if req.Mission != nil {
		mission = req.Mission
	}
	if req.Background != nil {
		mission = req.Background
	}

	profile, err := s.deps.Store.UpdateBank(r.Context(), bankID, name, mission, disposition)
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Error("update bank failed", "error", err, "bank_id", bankID)
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bankProfileToResponse(profile))
}

func (s *server) deleteBank(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	deletedCount, err := s.deps.Store.DeleteBank(r.Context(), bankID)
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Error("delete bank failed", "error", err, "bank_id", bankID)
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	msg := "Bank deleted successfully"
	resp := deleteResponse{
		Success:      true,
		Message:      &msg,
		DeletedCount: &deletedCount,
	}
	writeJSON(w, http.StatusOK, resp)
}
