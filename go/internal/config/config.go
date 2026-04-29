package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/singularity-ng/singularity-memory/go/internal/storageprofile"
)

const (
	defaultHost           = "127.0.0.1"
	defaultPort           = "8888"
	defaultDatabaseSchema = "public"
	defaultEmbedModel     = "qwen/qwen3-embedding-4b"
	// 0 means omit the OpenAI-compatible dimensions field and use the
	// embedding model's native output size. For Qwen3-Embedding-4B this is
	// 2560 dimensions, which is the first vchord profile to benchmark.
	defaultEmbedDimensions   = 0
	defaultEmbedBatchSize    = 32
	defaultRerankModel       = "cohere-rerank-v3"
	defaultRerankTopK        = 10
	defaultStorageProfile    = "vchord"
	defaultModelCatalogPath  = ".singularity-memory/model-catalog.json"
	defaultRetainBatchTokens = 8000
	defaultRRFK              = 60
	defaultRRFWeights        = "1.0,1.0,0.5,0.3"
)

type Config struct {
	Host           string
	Port           string
	DatabaseURL    string
	DatabaseSchema string
	MCPEnabled     bool

	// Embedding configuration
	EmbedGatewayURL string
	EmbedModel      string
	EmbedDimensions int
	EmbedBatchSize  int

	// Reranking configuration
	RerankGatewayURL string
	RerankModel      string
	RerankTopK       int

	// Storage profile
	StorageProfile storageprofile.Profile

	// Model catalog cache used by the HTTP API, SF export, and Charm TUI.
	ModelCatalogPath string

	// Feature flags parsed from SINGULARITY_FEATURE_* env vars
	FeatureFlags map[string]bool

	// Memory / retain configuration
	RetainBatchTokens int

	// RRF fusion configuration
	RRFK       int
	RRFWeights []float64
}

func FromEnv() Config {
	profile, _ := storageprofile.ParseProfile(getenv("SINGULARITY_STORAGE_PROFILE", defaultStorageProfile))

	return Config{
		Host:           getenv("SINGULARITY_HOST", defaultHost),
		Port:           getenv("SINGULARITY_PORT", defaultPort),
		DatabaseURL:    os.Getenv("SINGULARITY_DATABASE_URL"),
		DatabaseSchema: getenv("SINGULARITY_DATABASE_SCHEMA", defaultDatabaseSchema),
		MCPEnabled:     getenvBool("SINGULARITY_MCP_ENABLED", true),

		EmbedGatewayURL: getenv("SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL", ""),
		EmbedModel:      getenv("SINGULARITY_EMBEDDINGS_OPENAI_MODEL", defaultEmbedModel),
		EmbedDimensions: getenvInt("SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS", defaultEmbedDimensions),
		EmbedBatchSize:  getenvInt("SINGULARITY_EMBED_BATCH_SIZE", defaultEmbedBatchSize),

		RerankGatewayURL: getenv("SINGULARITY_RERANK_OPENAI_BASE_URL", ""),
		RerankModel:      getenv("SINGULARITY_RERANK_MODEL", defaultRerankModel),
		RerankTopK:       getenvInt("SINGULARITY_RERANK_TOP_K", defaultRerankTopK),

		StorageProfile:   profile,
		ModelCatalogPath: getenv("SINGULARITY_MODEL_CATALOG_PATH", defaultModelCatalogPath),
		FeatureFlags:     parseFeatureFlags(),

		RetainBatchTokens: getenvInt("SINGULARITY_RETAIN_BATCH_TOKENS", defaultRetainBatchTokens),
		RRFK:              getenvInt("SINGULARITY_RRF_K", defaultRRFK),
		RRFWeights:        parseRRFWeights(getenv("SINGULARITY_RRF_WEIGHTS", defaultRRFWeights)),
	}
}

func (c Config) Addr() string {
	return net.JoinHostPort(c.Host, c.Port)
}

func (c Config) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port == "" {
		return fmt.Errorf("port is required")
	}
	if _, err := strconv.Atoi(c.Port); err != nil {
		return fmt.Errorf("port must be numeric: %w", err)
	}
	if c.DatabaseSchema == "" {
		return fmt.Errorf("database schema is required")
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getenvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// parseFeatureFlags reads all environment variables starting with SINGULARITY_FEATURE_
// and returns a map of flag names to boolean values. All flags default to false.
func parseFeatureFlags() map[string]bool {
	flags := make(map[string]bool)
	prefix := "SINGULARITY_FEATURE_"
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		if len(pair) != 2 {
			continue
		}
		key := pair[0]
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(key, prefix))
		val, err := strconv.ParseBool(pair[1])
		if err != nil {
			continue
		}
		flags[name] = val
	}
	return flags
}

// parseRRFWeights parses a comma-separated list of floats.
// Defaults to [1.0, 1.0, 0.5, 0.3] if parsing fails.
func parseRRFWeights(raw string) []float64 {
	parts := strings.Split(raw, ",")
	weights := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return []float64{1.0, 1.0, 0.5, 0.3}
		}
		weights = append(weights, v)
	}
	if len(weights) == 0 {
		return []float64{1.0, 1.0, 0.5, 0.3}
	}
	return weights
}
