package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/singularity-ng/singularity-memory/go/internal/storageprofile"
	"gopkg.in/yaml.v3"
)

const (
	defaultHost           = "127.0.0.1"
	defaultPort           = "8888"
	defaultDatabaseSchema = "public"
	defaultEmbedModel     = "qwen/qwen3-embedding-4b"
	// 0 means omit the OpenAI-compatible dimensions field and use the
	// embedding model's native output size. For Qwen3-Embedding-4B this is
	// 2560 dimensions, which is the first vchord profile to benchmark.
	defaultEmbedDimensions         = 0
	defaultEmbedBatchSize          = 32
	defaultRerankModel             = "cohere-rerank-v3"
	defaultRerankTopK              = 10
	defaultStorageProfile          = "vchord"
	defaultModelCatalogPath        = ".singularity-memory/model-catalog.json"
	defaultModelDiscoveryStorePath = ".singularity-memory/model-discovery.json"
	defaultSecretSource            = "env"
	defaultSOPSSecretsPath         = "~/.dotfiles/secrets/api-keys.yaml"
	defaultSOPSConfigPath          = "~/.dotfiles/.sops.yaml"
	defaultRetainBatchTokens       = 8000
	defaultRRFK                    = 60
	defaultRRFWeights              = "1.0,1.0,0.5,0.3"
)

type ModelDiscoveryEndpoint struct {
	ID         string
	Name       string
	BaseURL    string
	SecretRef  string
	APIKey     string
	KeySource  string
	SecretHint string
}

type modelDiscoveryStore struct {
	SecretSource string                     `json:"secret_source"`
	Providers    []modelDiscoveryDefinition `json:"providers"`
}

type modelDiscoveryDefinition struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	BaseURL   string `json:"base_url"`
	SecretRef string `json:"secret_ref,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
}

type modelDiscoverySecretBundle struct {
	Values      map[string]string
	Definitions []modelDiscoveryDefinition
}

type Config struct {
	Host           string
	Port           string
	DatabaseURL    string
	DatabaseSchema string
	MCPEnabled     bool

	// Embedding configuration
	EmbedGatewayURL string
	EmbedAPIKey     string
	EmbedModel      string
	EmbedDimensions int
	EmbedBatchSize  int

	// Reranking configuration
	RerankGatewayURL string
	RerankAPIKey     string
	RerankModel      string
	RerankTopK       int

	// Storage profile
	StorageProfile storageprofile.Profile

	// Model catalog cache used by the HTTP API, SF export, and Charm TUI.
	ModelCatalogPath        string
	ModelDiscoveryStorePath string
	// Live OpenAI-compatible model discovery endpoints. Keys are resolved from
	// the configured secret source and never exposed by the HTTP API.
	ModelDiscoveryEndpoints    []ModelDiscoveryEndpoint
	ModelDiscoverySecretSource string
	ModelDiscoverySecretError  string
	ModelDiscoveryStoreError   string

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
	modelDiscoveryStorePath := getenv("SINGULARITY_MODEL_DISCOVERY_STORE_PATH", defaultModelDiscoveryStorePath)
	discoveryStore, storeErr := loadModelDiscoveryStore(modelDiscoveryStorePath)
	storeHint := ""
	if storeErr != nil {
		storeHint = storeErr.Error()
	}
	secretSource := discoveryStore.SecretSource
	if secretSource == "" {
		secretSource = getenv("SINGULARITY_MODEL_DISCOVERY_SECRET_SOURCE", defaultSecretSource)
	}
	secretBundle, secretErr := loadModelDiscoverySecrets(secretSource)
	secretHint := ""
	if secretErr != nil {
		secretHint = secretErr.Error()
	}
	definitions := discoveryStore.Providers
	if len(definitions) == 0 {
		definitions = secretBundle.Definitions
	}
	if len(definitions) == 0 {
		definitions = parseModelDiscoveryEndpointSpecs(os.Getenv("SINGULARITY_MODEL_DISCOVERY_ENDPOINTS"))
	}
	modelDiscoveryEndpoints := materializeModelDiscoveryEndpoints(
		definitions,
		secretSource,
		secretBundle.Values,
		secretHint,
	)

	return Config{
		Host:           getenv("SINGULARITY_HOST", defaultHost),
		Port:           getenv("SINGULARITY_PORT", defaultPort),
		DatabaseURL:    os.Getenv("SINGULARITY_DATABASE_URL"),
		DatabaseSchema: getenv("SINGULARITY_DATABASE_SCHEMA", defaultDatabaseSchema),
		MCPEnabled:     getenvBool("SINGULARITY_MCP_ENABLED", true),

		EmbedGatewayURL: getenv("SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL", ""),
		EmbedAPIKey:     firstNonEmpty(os.Getenv("SINGULARITY_EMBEDDINGS_OPENAI_API_KEY"), os.Getenv("LLM_MUX_API_KEY")),
		EmbedModel:      getenv("SINGULARITY_EMBEDDINGS_OPENAI_MODEL", defaultEmbedModel),
		EmbedDimensions: getenvInt("SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS", defaultEmbedDimensions),
		EmbedBatchSize:  getenvInt("SINGULARITY_EMBED_BATCH_SIZE", defaultEmbedBatchSize),

		RerankGatewayURL: getenv("SINGULARITY_RERANK_OPENAI_BASE_URL", ""),
		RerankAPIKey:     firstNonEmpty(os.Getenv("SINGULARITY_RERANK_OPENAI_API_KEY"), os.Getenv("SINGULARITY_EMBEDDINGS_OPENAI_API_KEY"), os.Getenv("LLM_MUX_API_KEY")),
		RerankModel:      getenv("SINGULARITY_RERANK_MODEL", defaultRerankModel),
		RerankTopK:       getenvInt("SINGULARITY_RERANK_TOP_K", defaultRerankTopK),

		StorageProfile:             profile,
		ModelCatalogPath:           getenv("SINGULARITY_MODEL_CATALOG_PATH", defaultModelCatalogPath),
		ModelDiscoveryStorePath:    modelDiscoveryStorePath,
		ModelDiscoveryEndpoints:    modelDiscoveryEndpoints,
		ModelDiscoverySecretSource: secretSource,
		ModelDiscoverySecretError:  secretHint,
		ModelDiscoveryStoreError:   storeHint,
		FeatureFlags:               parseFeatureFlags(),

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func loadModelDiscoveryStore(path string) (modelDiscoveryStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return modelDiscoveryStore{}, nil
	}
	body, err := os.ReadFile(expandHome(path))
	if err != nil {
		if os.IsNotExist(err) {
			return modelDiscoveryStore{}, nil
		}
		return modelDiscoveryStore{}, fmt.Errorf("model discovery store read failed")
	}
	var store modelDiscoveryStore
	if err := json.Unmarshal(body, &store); err != nil {
		return modelDiscoveryStore{}, fmt.Errorf("model discovery store parse failed")
	}
	store.Providers = compactModelDiscoveryDefinitions(store.Providers)
	return store, nil
}

func compactModelDiscoveryDefinitions(definitions []modelDiscoveryDefinition) []modelDiscoveryDefinition {
	out := make([]modelDiscoveryDefinition, 0, len(definitions))
	for _, definition := range definitions {
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Name = strings.TrimSpace(definition.Name)
		definition.BaseURL = strings.TrimSpace(definition.BaseURL)
		definition.SecretRef = strings.TrimSpace(definition.SecretRef)
		if definition.Disabled || definition.ID == "" || definition.BaseURL == "" {
			continue
		}
		if definition.Name == "" {
			definition.Name = definition.ID
		}
		out = append(out, definition)
	}
	return out
}

func parseModelDiscoveryEndpointSpecs(raw string) []modelDiscoveryDefinition {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	entries := strings.Split(raw, ";")
	definitions := make([]modelDiscoveryDefinition, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, "|")
		if len(parts) < 3 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		baseURL := strings.TrimSpace(parts[1])
		secretRef := strings.TrimSpace(parts[2])
		if id == "" || baseURL == "" {
			continue
		}
		name := id
		if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
			name = strings.TrimSpace(parts[3])
		}
		definitions = append(definitions, modelDiscoveryDefinition{
			ID:        id,
			Name:      name,
			BaseURL:   baseURL,
			SecretRef: secretRef,
		})
	}
	return definitions
}

func materializeModelDiscoveryEndpoints(definitions []modelDiscoveryDefinition, secretSource string, secrets map[string]string, secretHint string) []ModelDiscoveryEndpoint {
	definitions = compactModelDiscoveryDefinitions(definitions)
	endpoints := make([]ModelDiscoveryEndpoint, 0, len(definitions))
	for _, definition := range definitions {
		apiKey, keySource := resolveDiscoverySecret(definition.SecretRef, secretSource, secrets)
		endpoints = append(endpoints, ModelDiscoveryEndpoint{
			ID:         definition.ID,
			Name:       definition.Name,
			BaseURL:    definition.BaseURL,
			SecretRef:  definition.SecretRef,
			APIKey:     apiKey,
			KeySource:  keySource,
			SecretHint: secretHint,
		})
	}
	return endpoints
}

func resolveDiscoverySecret(secretRef, secretSource string, secrets map[string]string) (string, string) {
	if secretRef == "" {
		return "", ""
	}
	switch strings.ToLower(strings.TrimSpace(secretSource)) {
	case "sf-sops":
		return secrets[secretRef], "sf-sops"
	case "sf-sops,env", "sf-sops+env":
		if value := secrets[secretRef]; value != "" {
			return value, "sf-sops"
		}
		return os.Getenv(secretRef), "env"
	default:
		return os.Getenv(secretRef), "env"
	}
}

func loadModelDiscoverySecrets(secretSource string) (modelDiscoverySecretBundle, error) {
	switch strings.ToLower(strings.TrimSpace(secretSource)) {
	case "sf-sops", "sf-sops,env", "sf-sops+env":
		return loadSFSOPSSecrets(
			getenv("SINGULARITY_MODEL_DISCOVERY_SOPS_FILE", defaultSOPSSecretsPath),
			getenv("SINGULARITY_MODEL_DISCOVERY_SOPS_CONFIG", defaultSOPSConfigPath),
		)
	default:
		return modelDiscoverySecretBundle{Values: map[string]string{}}, nil
	}
}

func loadSFSOPSSecrets(secretsPath, configPath string) (modelDiscoverySecretBundle, error) {
	if _, err := exec.LookPath("sops"); err != nil {
		return modelDiscoverySecretBundle{Values: map[string]string{}}, fmt.Errorf("sops not found")
	}
	args := []string{}
	if configPath != "" {
		args = append(args, "--config", expandHome(configPath))
	}
	args = append(args, "-d", expandHome(secretsPath))
	out, err := exec.Command("sops", args...).Output()
	if err != nil {
		return modelDiscoverySecretBundle{Values: map[string]string{}}, fmt.Errorf("sf-sops decrypt failed")
	}
	return parseSFSOPSBundle(out)
}

func parseSFSOPSSecrets(data []byte) (map[string]string, error) {
	bundle, err := parseSFSOPSBundle(data)
	if err != nil {
		return nil, err
	}
	return bundle.Values, nil
}

func parseSFSOPSBundle(data []byte) (modelDiscoverySecretBundle, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return modelDiscoverySecretBundle{}, err
	}
	out := map[string]string{}
	sf, ok := asMap(doc["sf"])
	if !ok {
		return modelDiscoverySecretBundle{Values: out}, nil
	}
	for key, value := range sf {
		if key == "env" || key == "providers" || key == "model_discovery" {
			continue
		}
		addScalarSecret(out, key, value, "sf."+key)
	}
	if env, ok := asMap(sf["env"]); ok {
		for key, value := range env {
			addScalarSecret(out, key, value, "sf.env."+key)
		}
	}
	definitions := parseSFSOPSDiscoveryDefinitions(sf)
	if providers, ok := asMap(sf["providers"]); ok {
		for providerID, rawProvider := range providers {
			provider, ok := asMap(rawProvider)
			if !ok {
				continue
			}
			env, ok := asMap(provider["env"])
			if ok {
				for key, value := range env {
					addScalarSecret(out, key, value, "sf.providers."+providerID+".env."+key)
				}
			}
			for key, value := range provider {
				if key == "env" || key == "model_discovery" {
					continue
				}
				addScalarSecret(out, key, value, "sf.providers."+providerID+"."+key)
			}
		}
	}
	return modelDiscoverySecretBundle{
		Values:      out,
		Definitions: definitions,
	}, nil
}

func asMap(value any) (map[string]any, bool) {
	if typed, ok := value.(map[string]any); ok {
		return typed, true
	}
	return nil, false
}

func parseSFSOPSDiscoveryDefinitions(sf map[string]any) []modelDiscoveryDefinition {
	var definitions []modelDiscoveryDefinition
	if discovery, ok := asMap(sf["model_discovery"]); ok {
		definitions = append(definitions, parseProviderDefinitionList("", discovery["providers"])...)
	}
	if providers, ok := asMap(sf["providers"]); ok {
		for providerID, rawProvider := range providers {
			provider, ok := asMap(rawProvider)
			if !ok {
				continue
			}
			discovery, ok := asMap(provider["model_discovery"])
			if !ok {
				continue
			}
			if definition, ok := providerDefinitionFromMap(providerID, discovery); ok {
				definitions = append(definitions, definition)
			}
		}
	}
	return compactModelDiscoveryDefinitions(definitions)
}

func parseProviderDefinitionList(idHint string, value any) []modelDiscoveryDefinition {
	switch typed := value.(type) {
	case []any:
		definitions := make([]modelDiscoveryDefinition, 0, len(typed))
		for _, item := range typed {
			itemMap, ok := asMap(item)
			if !ok {
				continue
			}
			if definition, ok := providerDefinitionFromMap("", itemMap); ok {
				definitions = append(definitions, definition)
			}
		}
		return definitions
	case map[string]any:
		definitions := make([]modelDiscoveryDefinition, 0, len(typed))
		for providerID, item := range typed {
			itemMap, ok := asMap(item)
			if !ok {
				continue
			}
			if definition, ok := providerDefinitionFromMap(providerID, itemMap); ok {
				definitions = append(definitions, definition)
			}
		}
		return definitions
	case map[any]any:
		definitions := make([]modelDiscoveryDefinition, 0, len(typed))
		for rawProviderID, item := range typed {
			providerID := fmt.Sprint(rawProviderID)
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if definition, ok := providerDefinitionFromMap(providerID, itemMap); ok {
				definitions = append(definitions, definition)
			}
		}
		return definitions
	default:
		if itemMap, ok := asMap(value); ok && idHint != "" {
			if definition, ok := providerDefinitionFromMap(idHint, itemMap); ok {
				return []modelDiscoveryDefinition{definition}
			}
		}
		return nil
	}
}

func providerDefinitionFromMap(idHint string, value map[string]any) (modelDiscoveryDefinition, bool) {
	definition := modelDiscoveryDefinition{
		ID:        stringField(value, "id"),
		Name:      stringField(value, "name"),
		BaseURL:   firstStringField(value, "base_url", "baseURL", "url"),
		SecretRef: firstStringField(value, "secret_ref", "secretRef", "api_key_ref", "apiKeyRef"),
		Disabled:  boolField(value, "disabled"),
	}
	if definition.ID == "" {
		definition.ID = idHint
	}
	compact := compactModelDiscoveryDefinitions([]modelDiscoveryDefinition{definition})
	if len(compact) == 0 {
		return modelDiscoveryDefinition{}, false
	}
	return compact[0], true
}

func stringField(values map[string]any, key string) string {
	if value, ok := values[key]; ok && value != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

func firstStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(values, key); value != "" {
			return value
		}
	}
	return ""
}

func boolField(values map[string]any, key string) bool {
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(typed)
		return parsed
	default:
		return false
	}
}

func addScalarSecret(out map[string]string, key string, value any, aliases ...string) {
	key = strings.TrimSpace(key)
	if key == "" || value == nil {
		return
	}
	var rendered string
	switch v := value.(type) {
	case string:
		rendered = v
	case int, int64, float64, bool:
		rendered = fmt.Sprint(v)
	}
	if rendered == "" {
		return
	}
	out[key] = rendered
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias != "" {
			out[alias] = rendered
		}
	}
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home + path[1:]
	}
	return path
}
