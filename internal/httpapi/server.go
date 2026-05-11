package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/config"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/mcp"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/rerank"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

type Store interface {
	Ping(context.Context) error
	ListBanks(context.Context) ([]store.BankListItem, error)
	GetBank(ctx context.Context, bankID string) (*store.BankProfile, error)
	UpdateBank(ctx context.Context, bankID string, name *string, mission *string, disposition map[string]int) (*store.BankProfile, error)
	DeleteBank(ctx context.Context, bankID string) (int, error)

	// Memory unit CRUD
	InsertMemoryUnit(ctx context.Context, bankID string, unit *store.MemoryUnit) (string, error)
	GetMemoryUnit(ctx context.Context, bankID string, unitID string) (*store.MemoryUnit, error)
	DeleteMemoryUnit(ctx context.Context, bankID string, unitID string) error
	ListMemoryUnits(ctx context.Context, bankID string, limit int, offset int) ([]store.MemoryUnit, error)

	// Memory links
	InsertMemoryLink(ctx context.Context, link *store.MemoryLink) error

	// Entities
	UpsertEntity(ctx context.Context, bankID string, canonicalName string) (string, error)
	LinkUnitEntity(ctx context.Context, unitID string, entityID string) error
	RecentEntityUnitIDs(ctx context.Context, bankID string, entityID string, excludeUnitID string, limit int) ([]string, error)
	GetEntityObservations(ctx context.Context, bankID string, entityName string, limit int) ([]store.EntityObservation, error)

	// Chunk storage
	InsertChunk(ctx context.Context, bankID string, chunk *store.Chunk) (string, error)
	GetChunks(ctx context.Context, bankID string, documentID string) ([]store.Chunk, error)

	// Document upsert
	UpsertDocument(ctx context.Context, bankID string, documentID string, text string) error

	// Agent-brain page storage
	UpsertBrainPage(ctx context.Context, bankID string, input store.BrainPageInput) (*store.BrainPage, error)
	GetBrainPage(ctx context.Context, bankID string, slug string) (*store.BrainPage, error)
	ListBrainPages(ctx context.Context, bankID string, limit int) ([]store.BrainPage, error)
	AddBrainLink(ctx context.Context, bankID string, sourceID string, link store.BrainLink) error
	GetBrainLinks(ctx context.Context, bankID string, sourceID string, slug string, backlinks bool) ([]store.BrainLink, error)
	AddBrainTimelineEntry(ctx context.Context, bankID string, sourceID string, entry store.BrainTimelineEntry) (*store.BrainTimelineEntry, error)
	GetBrainTimeline(ctx context.Context, bankID string, sourceID string, slug string, limit int) ([]store.BrainTimelineEntry, error)
	EnqueueBrainJob(ctx context.Context, bankID string, input store.BrainJobInput) (*store.BrainJob, error)
	ListBrainJobs(ctx context.Context, bankID string, status string, limit int) ([]store.BrainJob, error)
	ClaimBrainJob(ctx context.Context, bankID string, kinds []string) (*store.BrainJob, error)
	CompleteBrainJob(ctx context.Context, bankID string, jobID string, status string, result map[string]any, jobErr *string) (*store.BrainJob, error)
	UpsertCoreMemoryBlock(ctx context.Context, block store.CoreMemoryBlock) (*store.CoreMemoryBlock, error)
	AppendCoreMemoryBlock(ctx context.Context, bankID string, blockName string, text string) (*store.CoreMemoryBlock, error)
	ReplaceCoreMemoryBlock(ctx context.Context, bankID string, blockName string, oldText string, newText string) (*store.CoreMemoryBlock, error)
	DeleteCoreMemoryBlock(ctx context.Context, bankID string, blockName string) (bool, error)
	ListCoreMemoryBlocks(ctx context.Context, bankID string) ([]store.CoreMemoryBlock, error)
	RunDeterministicConsolidation(ctx context.Context, bankID string, limit int) (*store.ConsolidationResult, error)
	ReflectAgentMemory(ctx context.Context, bankID string, limit int) (*store.Reflection, error)

	// Querier access for retrieval lanes
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Embedder is the interface satisfied by embed.Client.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

type Dependencies struct {
	Config       config.Config
	Store        Store
	Logger       *log.Logger
	EmbedClient  Embedder
	RerankClient *rerank.Client
	Version      string
	OpenAPIJSON  []byte
	MCPServer    *mcp.Server
}

func NewServer(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	server := &server{deps: deps}

	r.Get("/healthz", server.healthz)
	r.Get("/version", server.version)
	r.Get("/openapi.json", server.openapi)

	// Bank endpoints gated by "banks" feature flag
	r.Route("/v1", func(r chi.Router) {
		r.Use(featureFlagMiddleware(deps.Config.FeatureFlags, "banks"))
		r.Get("/banks", server.listBanks)
		r.Get("/default/banks", server.listBanks)
		r.Get("/default/banks/{bank_id}/profile", server.getBank)
		r.Put("/default/banks/{bank_id}/profile", server.updateBank)
		r.Put("/default/banks/{bank_id}", server.updateBank)
		r.Patch("/default/banks/{bank_id}", server.updateBank)
		r.Delete("/default/banks/{bank_id}", server.deleteBank)

		// Memory endpoints gated by "memories" feature flag
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/memories", server.retain)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/memories/", server.retain)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/memories/recall", server.recall)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/context", server.contextPacket)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/brain/pages", server.listBrainPages)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/brain/pages", server.upsertBrainPage)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/brain/links", server.addBrainLink)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/brain/links", server.getBrainLinks)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/brain/pages/{slug:.+}/links", server.getBrainLinks)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/brain/pages/{slug:.+}/backlinks", server.getBrainBacklinks)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/brain/timeline", server.addBrainTimeline)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/brain/timeline", server.getBrainTimeline)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/brain/pages/{slug:.+}/timeline", server.addBrainTimeline)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/brain/pages/{slug:.+}/timeline", server.getBrainTimeline)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/brain/jobs", server.enqueueBrainJob)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/brain/jobs", server.listBrainJobs)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/brain/jobs/claim", server.claimBrainJob)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/brain/jobs/{job_id}/complete", server.completeBrainJob)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/brain/pages/{slug:.+}", server.getBrainPage)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/core-memory", server.listCoreMemory)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Put("/default/banks/{bank_id}/core-memory/{block_name}", server.upsertCoreMemory)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Patch("/default/banks/{bank_id}/core-memory/{block_name}/append", server.appendCoreMemory)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Patch("/default/banks/{bank_id}/core-memory/{block_name}/replace", server.replaceCoreMemory)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Delete("/default/banks/{bank_id}/core-memory/{block_name}", server.deleteCoreMemory)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Post("/default/banks/{bank_id}/consolidate", server.consolidateMemory)
		r.With(featureFlagMiddleware(deps.Config.FeatureFlags, "memories")).
			Get("/default/banks/{bank_id}/reflect", server.reflectMemory)
	})

	// MCP JSON-RPC 2.0 endpoints
	if deps.MCPServer != nil {
		mcpServer := deps.MCPServer
		mcpServer.ToolBackend = newMCPToolBackend(server)
		// Multi-bank route: bank from X-Bank-Id header
		r.Handle("/mcp", mcpHandler(mcpServer, ""))
		r.Handle("/mcp/", mcpHandler(mcpServer, ""))
		// Single-bank route: bank from path parameter
		r.Handle("/mcp/{bank_id}", mcpHandler(mcpServer, ""))
		r.Handle("/mcp/{bank_id}/", mcpHandler(mcpServer, ""))
	}

	return r
}

// mcpHandler wraps the MCP server to inject the bank ID from either the
// X-Bank-Id header or the chi path parameter {bank_id}.
func mcpHandler(srv *mcp.Server, defaultBank string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bankID := chi.URLParam(r, "bank_id")
		if bankID == "" {
			bankID = r.Header.Get("X-Bank-Id")
		}
		// Clone the server with the resolved bank ID so the session store
		// picks it up as the default for this request.
		clone := *srv
		clone.BankID = bankID
		if clone.BankID == "" {
			clone.BankID = defaultBank
		}
		clone.ServeHTTP(w, r)
	}
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
		"service":           "operations-memory-go",
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
