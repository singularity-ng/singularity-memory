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
	defaultHost              = "127.0.0.1"
	defaultPort              = "8888"
	defaultDatabaseSchema    = "public"
	defaultEmbedModel        = "text-embedding-3-small"
	defaultEmbedDimensions   = 768
	defaultEmbedBatchSize    = 32
	defaultRerankModel       = "cohere-rerank-v3"
	defaultRerankTopK        = 10
	defaultStorageProfile    = "pgvector"
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

	// Feature flags parsed from SINGULARITY_FEATURE_* env vars
	FeatureFlags map[string]bool
}

func FromEnv() Config {
	profile, _ := storageprofile.ParseProfile(getenv("SINGULARITY_STORAGE_PROFILE", defaultStorageProfile))

	return Config{
		Host:           getenv("SINGULARITY_HOST", defaultHost),
		Port:           getenv("SINGULARITY_PORT", defaultPort),
		DatabaseURL:    os.Getenv("SINGULARITY_DATABASE_URL"),
		DatabaseSchema: getenv("SINGULARITY_DATABASE_SCHEMA", defaultDatabaseSchema),
		MCPEnabled:     getenvBool("SINGULARITY_MCP_ENABLED", true),

		EmbedGatewayURL: os.Getenv("SINGULARITY_EMBED_GATEWAY_URL"),
		EmbedModel:      getenv("SINGULARITY_EMBED_MODEL", defaultEmbedModel),
		EmbedDimensions: getenvInt("SINGULARITY_EMBED_DIMENSIONS", defaultEmbedDimensions),
		EmbedBatchSize:  getenvInt("SINGULARITY_EMBED_BATCH_SIZE", defaultEmbedBatchSize),

		RerankGatewayURL: os.Getenv("SINGULARITY_RERANK_GATEWAY_URL"),
		RerankModel:      getenv("SINGULARITY_RERANK_MODEL", defaultRerankModel),
		RerankTopK:       getenvInt("SINGULARITY_RERANK_TOP_K", defaultRerankTopK),

		StorageProfile: profile,
		FeatureFlags:   parseFeatureFlags(),
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
