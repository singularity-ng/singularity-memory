package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/config"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/embed"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/httpapi"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/mcp"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/rerank"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Error("operations-memory-go failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	host := fs.String("host", "", "bind host; defaults to OPS_MEMORY_HOST or 127.0.0.1")
	port := fs.String("port", "", "bind port; defaults to OPS_MEMORY_PORT or 8888")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg := config.FromEnv()
	if *host != "" {
		cfg.Host = *host
	}
	if *port != "" {
		cfg.Port = *port
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var db *store.Store
	if cfg.DatabaseURL != "" {
		opened, err := store.Open(ctx, cfg)
		if err != nil {
			return err
		}
		db = opened
		defer db.Close()
	} else {
		log.Warn("OPS_MEMORY_DATABASE_URL is not set; DB-backed endpoints will return 503")
	}

	var embedClient *embed.Client
	if cfg.EmbedGatewayURL != "" {
		embedClient = embed.NewClient(cfg, log.Default())
		log.Info("embedding client configured", "url", cfg.EmbedGatewayURL, "model", cfg.EmbedModel)
	} else {
		log.Warn("OPS_MEMORY_EMBEDDINGS_OPENAI_BASE_URL is not set; embedding endpoints will be unavailable")
	}

	var rerankClient *rerank.Client
	if cfg.RerankGatewayURL != "" {
		rerankClient = rerank.NewClient(cfg, log.Default())
		log.Info("rerank client configured", "url", cfg.RerankGatewayURL, "model", cfg.RerankModel)
	} else {
		log.Warn("OPS_MEMORY_RERANK_OPENAI_BASE_URL is not set; rerank endpoints will be unavailable")
	}

	var mcpServer *mcp.Server
	if cfg.MCPEnabled {
		mcpServer = mcp.NewServer()
		log.Info("mcp server enabled")
	}

	handler := httpapi.NewServer(httpapi.Dependencies{
		Config:       cfg,
		Store:        db,
		Logger:       log.Default(),
		EmbedClient:  embedClient,
		RerankClient: rerankClient,
		Version:      version,
		MCPServer:    mcpServer,
	})

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Log enabled feature flags at startup
	if len(cfg.FeatureFlags) > 0 {
		enabled := make([]string, 0, len(cfg.FeatureFlags))
		for name, on := range cfg.FeatureFlags {
			if on {
				enabled = append(enabled, name)
			}
		}
		if len(enabled) > 0 {
			log.Info("feature flags enabled", "flags", enabled)
		} else {
			log.Info("feature flags: none enabled")
		}
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("starting operations-memory-go", "addr", cfg.Addr(), "schema", cfg.DatabaseSchema)
		errs <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errs:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
