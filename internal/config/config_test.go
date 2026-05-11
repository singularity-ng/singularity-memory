package config

import (
	"os"
	"path/filepath"
	"testing"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/storageprofile"
)

func clearModelDiscoveryEnv(t *testing.T) {
	t.Setenv("OPS_MEMORY_MODEL_DISCOVERY_ENDPOINTS", "")
	t.Setenv("OPS_MEMORY_MODEL_DISCOVERY_SECRET_SOURCE", "")
	t.Setenv("OPS_MEMORY_MODEL_DISCOVERY_SOPS_FILE", "")
	t.Setenv("OPS_MEMORY_MODEL_DISCOVERY_SOPS_CONFIG", "")
	t.Setenv("OPS_MEMORY_MODEL_DISCOVERY_STORE_PATH", filepath.Join(t.TempDir(), "missing-model-discovery.json"))
}

func clearRuntimeEnv(t *testing.T) {
	for _, key := range []string{
		"OPS_MEMORY_HOST",
		"OPS_MEMORY_PORT",
		"OPS_MEMORY_DATABASE_URL",
		"OPS_MEMORY_DATABASE_SCHEMA",
		"OPS_MEMORY_MCP_ENABLED",
		"OPS_MEMORY_EMBEDDINGS_OPENAI_BASE_URL",
		"OPS_MEMORY_EMBEDDINGS_OPENAI_API_KEY",
		"OPS_MEMORY_EMBEDDINGS_OPENAI_MODEL",
		"OPS_MEMORY_EMBEDDINGS_OPENAI_DIMENSIONS",
		"OPS_MEMORY_EMBED_BATCH_SIZE",
		"OPS_MEMORY_RERANK_OPENAI_BASE_URL",
		"OPS_MEMORY_RERANK_OPENAI_API_KEY",
		"OPS_MEMORY_RERANK_MODEL",
		"OPS_MEMORY_RERANK_TOP_K",
		"OPS_MEMORY_STORAGE_PROFILE",
		"OPS_MEMORY_FEATURE_BANKS",
		"OPS_MEMORY_FEATURE_OBSERVATIONS",
		"OPS_MEMORY_FEATURE_WORKER",
		"OPS_MEMORY_FEATURE_FILE_UPLOAD_API",
	} {
		t.Setenv(key, "")
	}
}

func TestFromEnvDefaults(t *testing.T) {
	clearModelDiscoveryEnv(t)
	clearRuntimeEnv(t)

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
	if cfg.EmbedModel != "qwen/qwen3-embedding-4b" {
		t.Fatalf("EmbedModel = %q", cfg.EmbedModel)
	}
	if cfg.EmbedDimensions != 0 {
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
	if cfg.StorageProfile != storageprofile.VCHORD {
		t.Fatalf("StorageProfile = %q", cfg.StorageProfile)
	}
	if len(cfg.ModelDiscoveryEndpoints) != 0 {
		t.Fatalf("ModelDiscoveryEndpoints = %+v", cfg.ModelDiscoveryEndpoints)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	clearModelDiscoveryEnv(t)
	clearRuntimeEnv(t)
	t.Setenv("OPS_MEMORY_HOST", "0.0.0.0")
	t.Setenv("OPS_MEMORY_PORT", "9999")
	t.Setenv("OPS_MEMORY_DATABASE_URL", "postgres://example")
	t.Setenv("OPS_MEMORY_DATABASE_SCHEMA", "tenant_a")
	t.Setenv("OPS_MEMORY_MCP_ENABLED", "false")
	t.Setenv("OPS_MEMORY_EMBEDDINGS_OPENAI_BASE_URL", "https://llm-gateway.centralcloud.com/v1")
	t.Setenv("OPS_MEMORY_EMBEDDINGS_OPENAI_MODEL", "text-embedding-3-large")
	t.Setenv("OPS_MEMORY_EMBEDDINGS_OPENAI_DIMENSIONS", "1536")
	t.Setenv("OPS_MEMORY_EMBED_BATCH_SIZE", "64")
	t.Setenv("OPS_MEMORY_RERANK_OPENAI_BASE_URL", "https://llm-gateway.centralcloud.com/v1")
	t.Setenv("OPS_MEMORY_RERANK_MODEL", "bge-reranker-large")
	t.Setenv("OPS_MEMORY_RERANK_TOP_K", "20")
	t.Setenv("OPS_MEMORY_STORAGE_PROFILE", "vchord")

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
	if cfg.EmbedGatewayURL != "https://llm-gateway.centralcloud.com/v1" {
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
	if cfg.RerankGatewayURL != "https://llm-gateway.centralcloud.com/v1" {
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

func TestFeatureFlagsParsing(t *testing.T) {
	clearModelDiscoveryEnv(t)
	clearRuntimeEnv(t)
	t.Setenv("OPS_MEMORY_FEATURE_BANKS", "true")
	t.Setenv("OPS_MEMORY_FEATURE_OBSERVATIONS", "1")
	t.Setenv("OPS_MEMORY_FEATURE_WORKER", "false")
	t.Setenv("OPS_MEMORY_FEATURE_FILE_UPLOAD_API", "invalid")

	cfg := FromEnv()
	if !cfg.FeatureFlags["banks"] {
		t.Fatalf("expected banks flag true")
	}
	if !cfg.FeatureFlags["observations"] {
		t.Fatalf("expected observations flag true")
	}
	if cfg.FeatureFlags["worker"] {
		t.Fatalf("expected worker flag false")
	}
	if cfg.FeatureFlags["file_upload_api"] {
		t.Fatalf("expected file_upload_api flag false (invalid value)")
	}
	if _, ok := cfg.FeatureFlags["nonexistent"]; ok {
		t.Fatalf("expected nonexistent flag not present")
	}
}

func TestFeatureFlagsDefaultsToFalse(t *testing.T) {
	clearModelDiscoveryEnv(t)
	clearRuntimeEnv(t)

	cfg := FromEnv()
	if len(cfg.FeatureFlags) != 0 {
		t.Fatalf("expected no feature flags set, got %v", cfg.FeatureFlags)
	}
}

func TestModelDiscoveryEndpointsFromEnv(t *testing.T) {
	clearModelDiscoveryEnv(t)
	t.Setenv("KIMI_API_KEY", "test-key")
	t.Setenv("OPS_MEMORY_MODEL_DISCOVERY_ENDPOINTS", "kimi-coding|https://api.kimi.com/coding/v1|KIMI_API_KEY|Kimi Coding;ollama|http://localhost:11434/v1||Local Ollama")

	cfg := FromEnv()
	if len(cfg.ModelDiscoveryEndpoints) != 2 {
		t.Fatalf("endpoint count = %d", len(cfg.ModelDiscoveryEndpoints))
	}
	first := cfg.ModelDiscoveryEndpoints[0]
	if first.ID != "kimi-coding" || first.BaseURL != "https://api.kimi.com/coding/v1" || first.APIKey != "test-key" {
		t.Fatalf("unexpected first endpoint: %+v", first)
	}
	if first.KeySource != "env" {
		t.Fatalf("KeySource = %q", first.KeySource)
	}
	second := cfg.ModelDiscoveryEndpoints[1]
	if second.ID != "ollama" || second.SecretRef != "" || second.APIKey != "" {
		t.Fatalf("unexpected second endpoint: %+v", second)
	}
}

func TestModelDiscoveryEndpointsFromStore(t *testing.T) {
	clearModelDiscoveryEnv(t)
	storePath := filepath.Join(t.TempDir(), "model-discovery.json")
	t.Setenv("OPS_MEMORY_MODEL_DISCOVERY_STORE_PATH", storePath)
	t.Setenv("ZAI_API_KEY", "zai-test")
	if err := os.WriteFile(storePath, []byte(`{
  "secret_source": "env",
  "providers": [
    {
      "id": "zai",
      "name": "Z.AI",
      "base_url": "https://api.z.ai/api/coding/paas/v4",
      "secret_ref": "ZAI_API_KEY"
    }
  ]
}`), 0o600); err != nil {
		t.Fatalf("write store: %v", err)
	}

	cfg := FromEnv()
	if len(cfg.ModelDiscoveryEndpoints) != 1 {
		t.Fatalf("endpoint count = %d", len(cfg.ModelDiscoveryEndpoints))
	}
	endpoint := cfg.ModelDiscoveryEndpoints[0]
	if endpoint.ID != "zai" || endpoint.Name != "Z.AI" || endpoint.SecretRef != "ZAI_API_KEY" || endpoint.APIKey != "zai-test" {
		t.Fatalf("unexpected endpoint: %+v", endpoint)
	}
}

func TestParseSFSOPSSecretsOnlyReadsSFNamespace(t *testing.T) {
	secrets, err := parseSFSOPSSecrets([]byte(`
openrouter:
  OPENROUTER_API_KEY: global-openrouter
sf:
  OPENCODE_API_KEY: opencode-top
  env:
    KIMI_API_KEY: kimi
    EMPTY_KEY: ""
  providers:
    zai:
      env:
        ZAI_API_KEY: zai
    xiaomi:
      env:
        XIAOMI_API_KEY: xiaomi
`))
	if err != nil {
		t.Fatalf("parseSFSOPSSecrets returned error: %v", err)
	}
	if secrets["OPENROUTER_API_KEY"] != "" {
		t.Fatalf("read top-level OpenRouter key: %+v", secrets)
	}
	if secrets["OPENCODE_API_KEY"] != "opencode-top" {
		t.Fatalf("OPENCODE_API_KEY = %q", secrets["OPENCODE_API_KEY"])
	}
	if secrets["KIMI_API_KEY"] != "kimi" || secrets["ZAI_API_KEY"] != "zai" || secrets["XIAOMI_API_KEY"] != "xiaomi" {
		t.Fatalf("missing sf scoped secrets: %+v", secrets)
	}
	if secrets["sf.env.KIMI_API_KEY"] != "kimi" || secrets["sf.providers.zai.env.ZAI_API_KEY"] != "zai" {
		t.Fatalf("missing sf path aliases: %+v", secrets)
	}
	if _, ok := secrets["EMPTY_KEY"]; ok {
		t.Fatalf("empty key should be ignored")
	}
}

func TestParseSFSOPSModelDiscoveryDefinitions(t *testing.T) {
	bundle, err := parseSFSOPSBundle([]byte(`
sf:
  env:
    ZAI_API_KEY: zai
    MISTRAL_API_KEY: mistral
  model_discovery:
    providers:
      - id: zai
        name: Z.AI
        base_url: https://api.z.ai/api/coding/paas/v4
        secret_ref: sf.env.ZAI_API_KEY
  providers:
    mistral:
      env:
        MISTRAL_API_KEY: mistral
      model_discovery:
        name: Mistral
        base_url: https://api.mistral.ai/v1
        secret_ref: sf.providers.mistral.env.MISTRAL_API_KEY
`))
	if err != nil {
		t.Fatalf("parseSFSOPSBundle returned error: %v", err)
	}
	if len(bundle.Definitions) != 2 {
		t.Fatalf("definition count = %d: %+v", len(bundle.Definitions), bundle.Definitions)
	}
	endpoints := materializeModelDiscoveryEndpoints(bundle.Definitions, "sf-sops", bundle.Values, "")
	if len(endpoints) != 2 {
		t.Fatalf("endpoint count = %d", len(endpoints))
	}
	for _, endpoint := range endpoints {
		if endpoint.APIKey == "" || endpoint.KeySource != "sf-sops" {
			t.Fatalf("endpoint did not resolve from sf-sops: %+v", endpoint)
		}
	}
}
