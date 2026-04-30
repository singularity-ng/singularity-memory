package httpapi

import "net/http"

func (s *server) getModelCatalog(w http.ResponseWriter, r *http.Request) {
	if s.deps.ModelCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "model catalog is not configured")
		return
	}
	writeJSON(w, http.StatusOK, s.deps.ModelCatalog.Snapshot())
}

func (s *server) syncModelCatalog(w http.ResponseWriter, r *http.Request) {
	if s.deps.ModelCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "model catalog is not configured")
		return
	}
	catalog, err := s.deps.ModelCatalog.Refresh(r.Context())
	if err != nil {
		if s.deps.Logger != nil {
			s.deps.Logger.Error("model catalog sync failed", "error", err)
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

func (s *server) exportSFModelCatalog(w http.ResponseWriter, r *http.Request) {
	if s.deps.ModelCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "model catalog is not configured")
		return
	}
	writeJSON(w, http.StatusOK, s.deps.ModelCatalog.SFExport())
}
