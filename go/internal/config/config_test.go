package config

import (
	"testing"

	"github.com/singularity-ng/singularity-memory/go/internal/storageprofile"
)

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("SINGULARITY_HOST", "")
	t.Setenv("SINGULARITY_PORT", "")
	t.Setenv("SINGULARITY_DATABASE_URL", "")
	t.Setenv("SINGULARITY_DATABASE_SCHEMA", "")
	t.Setenv("SINGULARITY_MCP_ENABLED", "")
	t.Setenv("SINGULARITY_EMBED_GATEWAY_URL", "")
	t.Setenv("SINGULARITY_EMBED_MODEL", "")
	t.Setenv("SINGULARITY_EMBED_DIMENSIONS", "")
	t.Setenv("SINGULARITY_EMBED_BATCH_SIZE", "")
	t.Setenv("SINGULARITY_RERANK_GATEWAY_URL", "")
	t.Setenv("SINGULARITY_RERANK_MODEL", "")
	t.Setenv("SINGULARITY_RERANK_TOP_K", "")
	t.Setenv("SINGULARITY_STORAGE_PROFILE", "")

	cfg := FromEnv()
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("Host = %q", cfg.Host)
	}
	if cfg.Port != "8888" {
		t.Fatalf("Port = %q", cfg.Port)
	}
	if cfg.DatabaseSchema != "public" {
		t.Fatalf("DatabaseSchema = %q", cfg.DatabaseSchema)
	}
	if !cfg.MCPEnabled {
		t.Fatalf("MCPEnabled = false")
	}
	if cfg.EmbedModel != "text-embedding-3-small" {
		t.Fatalf("EmbedModel = %q", cfg.EmbedModel)
	}
	if cfg.EmbedDimensions != 768 {
		t.Fatalf("EmbedDimensions = %d", cfg.EmbedDimensions)
	}
	if cfg.EmbedBatchSize != 32 {
		t.Fatalf("EmbedBatchSize = %d", cfg.EmbedBatchSize)
	}
	if cfg.RerankModel != "cohere-rerank-v3" {
		t.Fatalf("RerankModel = %q", cfg.RerankModel)
	}
	if cfg.RerankTopK != 10 {
		t.Fatalf("RerankTopK = %d", cfg.RerankTopK)
	}
	if cfg.StorageProfile != storageprofile.PGVECTOR {
		t.Fatalf("StorageProfile = %q", cfg.StorageProfile)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("SINGULARITY_HOST", "0.0.0.0")
	t.Setenv("SINGULARITY_PORT", "9999")
	t.Setenv("SINGULARITY_DATABASE_URL", "postgres://example")
	t.Setenv("SINGULARITY_DATABASE_SCHEMA", "tenant_a")
	t.Setenv("SINGULARITY_MCP_ENABLED", "false")
	t.Setenv("SINGULARITY_EMBED_GATEWAY_URL", "http://embed:8080")
	t.Setenv("SINGULARITY_EMBED_MODEL", "text-embedding-3-large")
	t.Setenv("SINGULARITY_EMBED_DIMENSIONS", "1536")
	t.Setenv("SINGULARITY_EMBED_BATCH_SIZE", "64")
	t.Setenv("SINGULARITY_RERANK_GATEWAY_URL", "http://rerank:8080")
	t.Setenv("SINGULARITY_RERANK_MODEL", "bge-reranker-large")
	t.Setenv("SINGULARITY_RERANK_TOP_K", "20")
	t.Setenv("SINGULARITY_STORAGE_PROFILE", "vchord")

	cfg := FromEnv()
	if cfg.Host != "0.0.0.0" || cfg.Port != "9999" {
		t.Fatalf("unexpected addr config: %+v", cfg)
	}
	if cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.DatabaseSchema != "tenant_a" {
		t.Fatalf("DatabaseSchema = %q", cfg.DatabaseSchema)
	}
	if cfg.MCPEnabled {
		t.Fatalf("MCPEnabled = true")
	}
	if cfg.EmbedGatewayURL != "http://embed:8080" {
		t.Fatalf("EmbedGatewayURL = %q", cfg.EmbedGatewayURL)
	}
	if cfg.EmbedModel != "text-embedding-3-large" {
		t.Fatalf("EmbedModel = %q", cfg.EmbedModel)
	}
	if cfg.EmbedDimensions != 1536 {
		t.Fatalf("EmbedDimensions = %d", cfg.EmbedDimensions)
	}
	if cfg.EmbedBatchSize != 64 {
		t.Fatalf("EmbedBatchSize = %d", cfg.EmbedBatchSize)
	}
	if cfg.RerankGatewayURL != "http://rerank:8080" {
		t.Fatalf("RerankGatewayURL = %q", cfg.RerankGatewayURL)
	}
	if cfg.RerankModel != "bge-reranker-large" {
		t.Fatalf("RerankModel = %q", cfg.RerankModel)
	}
	if cfg.RerankTopK != 20 {
		t.Fatalf("RerankTopK = %d", cfg.RerankTopK)
	}
	if cfg.StorageProfile != storageprofile.VCHORD {
		t.Fatalf("StorageProfile = %q", cfg.StorageProfile)
	}
}
