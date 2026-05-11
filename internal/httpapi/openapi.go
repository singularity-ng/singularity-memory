package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

func (s *server) openapi(w http.ResponseWriter, r *http.Request) {
	spec := s.deps.OpenAPIJSON
	if len(spec) == 0 {
		loaded, err := LoadOpenAPIJSON()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "openapi contract is not available")
			return
		}
		spec = loaded
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes.TrimSpace(spec))
	_, _ = w.Write([]byte("\n"))
}

func LoadOpenAPIJSON() ([]byte, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "openapi.json")
		if b, err := os.ReadFile(candidate); err == nil {
			return b, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	return nil, errors.New("openapi.json not found")
}
