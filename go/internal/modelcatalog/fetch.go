package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	CatwalkURL   = "https://catwalk.charm.land/v2/providers"
	ModelsDevURL = "https://models.dev/api.json"
)

type Fetcher struct {
	Client        *http.Client
	LiveProviders []LiveProvider
}

func (f Fetcher) Fetch(ctx context.Context) Catalog {
	now := time.Now().UTC()
	catalog := Catalog{GeneratedAt: now}
	f.fetchCatwalk(ctx, &catalog)
	f.fetchModelsDev(ctx, &catalog)
	for _, provider := range f.LiveProviders {
		f.fetchLiveProvider(ctx, &catalog, provider)
	}
	sortCatalog(&catalog)
	return catalog
}

func (f Fetcher) httpClient() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

type catwalkProvider struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	DefaultLargeModelID string         `json:"default_large_model_id"`
	DefaultSmallModelID string         `json:"default_small_model_id"`
	Models              []catwalkModel `json:"models"`
}

type catwalkModel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	CostPer1MIn         float64  `json:"cost_per_1m_in"`
	CostPer1MOut        float64  `json:"cost_per_1m_out"`
	ContextWindow       int64    `json:"context_window"`
	DefaultMaxTokens    int64    `json:"default_max_tokens"`
	CanReason           bool     `json:"can_reason"`
	ReasoningLevels     []string `json:"reasoning_levels"`
	SupportsAttachments bool     `json:"supports_attachments"`
}

func (f Fetcher) fetchCatwalk(ctx context.Context, catalog *Catalog) {
	status := SourceStatus{Name: "catwalk", URL: CatwalkURL, FetchedAt: time.Now().UTC()}
	defer func() {
		catalog.Sources = append(catalog.Sources, status)
	}()

	var providers []catwalkProvider
	if err := fetchJSON(ctx, f.httpClient(), CatwalkURL, &providers); err != nil {
		status.Error = err.Error()
		return
	}
	status.OK = true
	status.ProviderN = len(providers)
	for _, provider := range providers {
		catalog.Providers = append(catalog.Providers, Provider{
			ID:                  provider.ID,
			Name:                provider.Name,
			Source:              "catwalk",
			DefaultLargeModelID: provider.DefaultLargeModelID,
			DefaultSmallModelID: provider.DefaultSmallModelID,
			ModelCount:          len(provider.Models),
		})
		status.ModelCount += len(provider.Models)
		for _, m := range provider.Models {
			identity := Normalize(provider.ID, m.ID, m.Name)
			catalog.Models = append(catalog.Models, Model{
				ProviderID:       provider.ID,
				ID:               m.ID,
				Name:             m.Name,
				Source:           "catwalk",
				CanonicalSlug:    identity.CanonicalSlug,
				Family:           identity.Family,
				Version:          identity.Version,
				SizeClass:        identity.SizeClass,
				ParamBillions:    identity.ParamBillions,
				ContextWindow:    m.ContextWindow,
				DefaultMaxTokens: m.DefaultMaxTokens,
				CostPer1MIn:      m.CostPer1MIn,
				CostPer1MOut:     m.CostPer1MOut,
				CanReason:        m.CanReason || len(m.ReasoningLevels) > 0,
				SupportsImages:   m.SupportsAttachments,
			})
		}
	}
}

type modelsDevProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Reasoning bool   `json:"reasoning"`
	Limit     struct {
		Context int64 `json:"context"`
		Output  int64 `json:"output"`
	} `json:"limit"`
	Cost struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"cost"`
	Modalities struct {
		Input []string `json:"input"`
	} `json:"modalities"`
}

func (f Fetcher) fetchModelsDev(ctx context.Context, catalog *Catalog) {
	status := SourceStatus{Name: "models.dev", URL: ModelsDevURL, FetchedAt: time.Now().UTC()}
	defer func() {
		catalog.Sources = append(catalog.Sources, status)
	}()

	var raw map[string]json.RawMessage
	if err := fetchJSON(ctx, f.httpClient(), ModelsDevURL, &raw); err != nil {
		status.Error = err.Error()
		return
	}
	status.OK = true
	for providerID, body := range raw {
		if providerID == "$schema" || providerID == "_meta" {
			continue
		}
		var provider modelsDevProvider
		if err := json.Unmarshal(body, &provider); err != nil || len(provider.Models) == 0 {
			continue
		}
		if provider.ID == "" {
			provider.ID = providerID
		}
		catalog.Providers = append(catalog.Providers, Provider{
			ID:         provider.ID,
			Name:       provider.Name,
			Source:     "models.dev",
			ModelCount: len(provider.Models),
		})
		status.ProviderN++
		status.ModelCount += len(provider.Models)
		for id, m := range provider.Models {
			if m.ID == "" {
				m.ID = id
			}
			identity := Normalize(provider.ID, m.ID, m.Name)
			catalog.Models = append(catalog.Models, Model{
				ProviderID:       provider.ID,
				ID:               m.ID,
				Name:             m.Name,
				Source:           "models.dev",
				CanonicalSlug:    identity.CanonicalSlug,
				Family:           identity.Family,
				Version:          identity.Version,
				SizeClass:        identity.SizeClass,
				ParamBillions:    identity.ParamBillions,
				ContextWindow:    m.Limit.Context,
				DefaultMaxTokens: m.Limit.Output,
				CostPer1MIn:      m.Cost.Input,
				CostPer1MOut:     m.Cost.Output,
				CanReason:        m.Reasoning,
				SupportsImages:   contains(m.Modalities.Input, "image"),
			})
		}
	}
}

type liveModelsEnvelope struct {
	Data   []json.RawMessage `json:"data"`
	Models []json.RawMessage `json:"models"`
}

type liveModel struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Model            string `json:"model"`
	ContextWindow    int64  `json:"context_window"`
	DefaultMaxTokens int64  `json:"default_max_tokens"`
}

func (f Fetcher) fetchLiveProvider(ctx context.Context, catalog *Catalog, provider LiveProvider) {
	provider.ID = strings.TrimSpace(provider.ID)
	provider.BaseURL = strings.TrimSpace(provider.BaseURL)
	provider.SecretRef = strings.TrimSpace(provider.SecretRef)
	if provider.ID == "" || provider.BaseURL == "" {
		return
	}

	modelsURL := liveModelsURL(provider.BaseURL)
	status := SourceStatus{
		Name:        "live:" + provider.ID,
		URL:         safeStatusURL(modelsURL),
		FetchedAt:   time.Now().UTC(),
		AuthRef:     provider.SecretRef,
		AuthSource:  provider.KeySource,
		AuthPresent: provider.APIKey != "",
	}
	if status.AuthSource == "" && provider.SecretRef != "" {
		status.AuthSource = "configured"
	}
	defer func() {
		catalog.Sources = append(catalog.Sources, status)
	}()

	if provider.SecretRef != "" && provider.APIKey == "" {
		status.Error = fmt.Sprintf("secret %s is not available from %s", provider.SecretRef, sourceLabel(status.AuthSource))
		if provider.SecretHint != "" {
			status.Error = fmt.Sprintf("%s: %s", status.Error, provider.SecretHint)
		}
		return
	}

	models, err := fetchLiveModels(ctx, f.httpClient(), modelsURL, provider.APIKey)
	if err != nil {
		status.Error = err.Error()
		return
	}
	status.OK = true
	status.ProviderN = 1
	status.ModelCount = len(models)
	name := provider.Name
	if name == "" {
		name = provider.ID
	}
	catalog.Providers = append(catalog.Providers, Provider{
		ID:         provider.ID,
		Name:       name,
		Source:     status.Name,
		ModelCount: len(models),
	})
	for _, m := range models {
		if m.ID == "" {
			continue
		}
		if m.Name == "" {
			m.Name = m.ID
		}
		identity := Normalize(provider.ID, m.ID, m.Name)
		catalog.Models = append(catalog.Models, Model{
			ProviderID:       provider.ID,
			ID:               m.ID,
			Name:             m.Name,
			Source:           status.Name,
			CanonicalSlug:    identity.CanonicalSlug,
			Family:           identity.Family,
			Version:          identity.Version,
			SizeClass:        identity.SizeClass,
			ParamBillions:    identity.ParamBillions,
			ContextWindow:    m.ContextWindow,
			DefaultMaxTokens: m.DefaultMaxTokens,
		})
	}
}

func fetchJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s returned %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func fetchLiveModels(ctx context.Context, client *http.Client, modelsURL, apiKey string) ([]liveModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s returned %s", safeStatusURL(modelsURL), resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return decodeLiveModels(body)
}

func decodeLiveModels(body []byte) ([]liveModel, error) {
	var envelope liveModelsEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	items := envelope.Data
	if len(items) == 0 {
		items = envelope.Models
	}
	if len(items) == 0 {
		var rawArray []json.RawMessage
		if err := json.Unmarshal(body, &rawArray); err == nil {
			items = rawArray
		}
	}
	models := make([]liveModel, 0, len(items))
	for _, item := range items {
		var id string
		if err := json.Unmarshal(item, &id); err == nil {
			id = strings.TrimSpace(id)
			if id != "" {
				models = append(models, liveModel{ID: id, Name: id})
			}
			continue
		}
		var model liveModel
		if err := json.Unmarshal(item, &model); err != nil {
			continue
		}
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" {
			model.ID = strings.TrimSpace(model.Model)
		}
		if model.ID == "" {
			model.ID = strings.TrimSpace(model.Name)
		}
		if model.ID == "" {
			continue
		}
		models = append(models, model)
	}
	return models, nil
}

func liveModelsURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	parsed, err := url.Parse(baseURL)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		if strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/api/tags") {
			return parsed.String()
		}
		if strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/models") {
			return parsed.String()
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/models"
		return parsed.String()
	}
	if strings.HasSuffix(strings.TrimRight(baseURL, "/"), "/models") {
		return baseURL
	}
	return strings.TrimRight(baseURL, "/") + "/models"
}

func safeStatusURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	for _, key := range []string{"key", "api_key", "apikey", "token", "access_token"} {
		if query.Has(key) {
			query.Set(key, "redacted")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sourceLabel(source string) string {
	if source == "" {
		return "configured source"
	}
	return source
}

func sortCatalog(catalog *Catalog) {
	sort.Slice(catalog.Providers, func(i, j int) bool {
		if catalog.Providers[i].ID == catalog.Providers[j].ID {
			return catalog.Providers[i].Source < catalog.Providers[j].Source
		}
		return catalog.Providers[i].ID < catalog.Providers[j].ID
	})
	sort.Slice(catalog.Models, func(i, j int) bool {
		a, b := catalog.Models[i], catalog.Models[j]
		if a.ProviderID == b.ProviderID {
			return a.ID < b.ID
		}
		return a.ProviderID < b.ProviderID
	})
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
