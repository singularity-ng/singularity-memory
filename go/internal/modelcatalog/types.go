package modelcatalog

import "time"

type Catalog struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Sources     []SourceStatus `json:"sources"`
	Providers   []Provider     `json:"providers"`
	Models      []Model        `json:"models"`
}

type LiveProvider struct {
	ID         string
	Name       string
	BaseURL    string
	SecretRef  string
	APIKey     string
	KeySource  string
	SecretHint string
}

type SourceStatus struct {
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	OK          bool      `json:"ok"`
	Error       string    `json:"error,omitempty"`
	FetchedAt   time.Time `json:"fetched_at"`
	ProviderN   int       `json:"provider_count"`
	ModelCount  int       `json:"model_count"`
	AuthRef     string    `json:"auth_ref,omitempty"`
	AuthSource  string    `json:"auth_source,omitempty"`
	AuthPresent bool      `json:"auth_present,omitempty"`
}

type Provider struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Source              string `json:"source"`
	DefaultLargeModelID string `json:"default_large_model_id,omitempty"`
	DefaultSmallModelID string `json:"default_small_model_id,omitempty"`
	ModelCount          int    `json:"model_count"`
}

type Model struct {
	ProviderID       string   `json:"provider_id"`
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Source           string   `json:"source"`
	CanonicalSlug    string   `json:"canonical_slug"`
	Family           string   `json:"family"`
	Version          string   `json:"version,omitempty"`
	SizeClass        string   `json:"size_class,omitempty"`
	ParamBillions    *float64 `json:"param_billions,omitempty"`
	ContextWindow    int64    `json:"context_window,omitempty"`
	DefaultMaxTokens int64    `json:"default_max_tokens,omitempty"`
	CostPer1MIn      float64  `json:"cost_per_1m_in,omitempty"`
	CostPer1MOut     float64  `json:"cost_per_1m_out,omitempty"`
	CanReason        bool     `json:"can_reason"`
	SupportsImages   bool     `json:"supports_images"`
}

type SFExport struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Models      []SFModelOverlay `json:"models"`
	Policy      SFPolicy         `json:"policy"`
}

type SFModelOverlay struct {
	ProviderID    string   `json:"provider_id"`
	WireModelID   string   `json:"wire_model_id"`
	CanonicalSlug string   `json:"canonical_slug"`
	Family        string   `json:"family"`
	Version       string   `json:"version,omitempty"`
	SizeClass     string   `json:"size_class,omitempty"`
	ParamBillions *float64 `json:"param_billions,omitempty"`
	CanReason     bool     `json:"can_reason"`
	ContextWindow int64    `json:"context_window,omitempty"`
}

type SFPolicy struct {
	JudgmentMinimumSizeClass   string              `json:"judgment_minimum_size_class"`
	TinySizeMaxBillions        int                 `json:"tiny_size_max_billions"`
	SmallSizeMaxBillions       int                 `json:"small_size_max_billions"`
	ProviderModelAllow         map[string][]string `json:"provider_model_allow,omitempty"`
	PreferredProvidersByFamily map[string][]string `json:"preferred_providers_by_family,omitempty"`
}
