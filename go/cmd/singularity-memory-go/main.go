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

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/embed"
	"github.com/singularity-ng/singularity-memory/go/internal/httpapi"
	"github.com/singularity-ng/singularity-memory/go/internal/modelcatalog"
	"github.com/singularity-ng/singularity-memory/go/internal/rerank"
	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Error("singularity-memory-go failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	host := fs.String("host", "", "bind host; defaults to SINGULARITY_HOST or 127.0.0.1")
	port := fs.String("port", "", "bind port; defaults to SINGULARITY_PORT or 8888")
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
		log.Warn("SINGULARITY_DATABASE_URL is not set; DB-backed endpoints will return 503")
	}

	var embedClient *embed.Client
	if cfg.EmbedGatewayURL != "" {
		embedClient = embed.NewClient(cfg, log.Default())
		log.Info("embedding client configured", "url", cfg.EmbedGatewayURL, "model", cfg.EmbedModel)
	} else {
		log.Warn("SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL is not set; embedding endpoints will be unavailable")
	}

	var rerankClient *rerank.Client
	if cfg.RerankGatewayURL != "" {
		rerankClient = rerank.NewClient(cfg, log.Default())
		log.Info("rerank client configured", "url", cfg.RerankGatewayURL, "model", cfg.RerankModel)
	} else {
		log.Warn("SINGULARITY_RERANK_OPENAI_BASE_URL is not set; rerank endpoints will be unavailable")
	}

	modelCatalog := modelcatalog.NewService(cfg.ModelCatalogPath, modelcatalog.Fetcher{})
	log.Info("model catalog configured", "path", cfg.ModelCatalogPath)

	handler := httpapi.NewServer(httpapi.Dependencies{
		Config:       cfg,
		Store:        db,
		Logger:       log.Default(),
		EmbedClient:  embedClient,
		RerankClient: rerankClient,
		ModelCatalog: modelCatalog,
		Version:      version,
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
		log.Info("starting singularity-memory-go", "addr", cfg.Addr(), "schema", cfg.DatabaseSchema)
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
