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
	GetBank(ctx context.Context, bankID string) (*store.BankProfile, error)
	UpdateBank(ctx context.Context, bankID string, name *string, mission *string, disposition map[string]int) (*store.BankProfile, error)
	DeleteBank(ctx context.Context, bankID string) (int, error)
}

type Dependencies struct {
	Config       config.Config
	Store        Store
	Logger       *log.Logger
	EmbedClient  *embed.Client
	RerankClient *rerank.Client
	Version      string
}

func NewServer(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	server := &server{deps: deps}

	r.Get("/healthz", server.healthz)
	r.Get("/version", server.version)

	// Bank endpoints gated by "banks" feature flag
	r.Route("/v1", func(r chi.Router) {
		r.Use(featureFlagMiddleware(deps.Config.FeatureFlags, "banks"))
		r.Get("/banks", server.listBanks)
		r.Get("/default/banks", server.listBanks)
		r.Get("/default/banks/{bank_id}/profile", server.getBank)
		r.Put("/default/banks/{bank_id}", server.updateBank)
		r.Patch("/default/banks/{bank_id}", server.updateBank)
		r.Delete("/default/banks/{bank_id}", server.deleteBank)
	})

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
		"ok":                ok,
		"service":           "singularity-memory-go",
		"database":          db,
		"database_schema":   s.deps.Config.DatabaseSchema,
		"mcp_enabled":       s.deps.Config.MCPEnabled,
		"storage_profile":   s.deps.Config.StorageProfile.String(),
		"embed_configured":  s.deps.Config.EmbedGatewayURL != "",
		"rerank_configured": s.deps.Config.RerankGatewayURL != "",
	})
}

func (s *server) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"api_version": s.deps.Version,
		"features": map[string]bool{
			"observations":    s.deps.Config.FeatureFlags["observations"],
			"mcp":             s.deps.Config.MCPEnabled,
			"worker":          s.deps.Config.FeatureFlags["worker"],
			"bank_config_api": s.deps.Config.FeatureFlags["bank_config_api"],
			"file_upload_api": s.deps.Config.FeatureFlags["file_upload_api"],
		},
	})
}

// featureFlagMiddleware returns a middleware that returns 404 if the named
// feature flag is not enabled. All flags default to false.
func featureFlagMiddleware(flags map[string]bool, name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !flags[name] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
