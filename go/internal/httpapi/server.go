package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/embed"
	"github.com/singularity-ng/singularity-memory/go/internal/rerank"
	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

type Store interface {
	Ping(context.Context) error
	ListBanks(context.Context) ([]store.BankListItem, error)
}

type Dependencies struct {
	Config      config.Config
	Store       Store
	Logger      *log.Logger
	EmbedClient *embed.Client
	RerankClient *rerank.Client
}

func NewServer(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	server := &server{deps: deps}

	r.Get("/healthz", server.healthz)
	r.Get("/v1/banks", server.listBanks)
	r.Get("/v1/default/banks", server.listBanks)

	return r
}

type server struct {
	deps Dependencies
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	db := "not_configured"
	ok := false
	if s.deps.Store != nil {
		if err := s.deps.Store.Ping(ctx); err == nil {
			db = "ok"
			ok = true
		} else {
			db = "error"
		}
	}

	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":               ok,
		"service":          "singularity-memory-go",
		"database":         db,
		"database_schema":  s.deps.Config.DatabaseSchema,
		"mcp_enabled":      s.deps.Config.MCPEnabled,
		"storage_profile":  s.deps.Config.StorageProfile.String(),
		"embed_configured": s.deps.Config.EmbedGatewayURL != "",
		"rerank_configured": s.deps.Config.RerankGatewayURL != "",
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
