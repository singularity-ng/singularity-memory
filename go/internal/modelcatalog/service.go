package modelcatalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Service struct {
	mu      sync.RWMutex
	path    string
	fetcher Fetcher
	catalog Catalog
}

func NewService(path string, fetcher Fetcher) *Service {
	s := &Service{path: path, fetcher: fetcher}
	_ = s.Load()
	return s
}

func (s *Service) Load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return err
	}
	s.mu.Lock()
	s.catalog = catalog
	s.mu.Unlock()
	return nil
}

func (s *Service) Snapshot() Catalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneCatalog(s.catalog)
}

func (s *Service) Refresh(ctx context.Context) (Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	catalog := s.fetcher.Fetch(ctx)
	s.mu.Lock()
	s.catalog = catalog
	s.mu.Unlock()
	return cloneCatalog(catalog), s.Save()
}

func (s *Service) Save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	s.mu.RLock()
	data, err := json.MarshalIndent(s.catalog, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o644)
}

func (s *Service) SFExport() SFExport {
	catalog := s.Snapshot()
	models := make([]SFModelOverlay, 0, len(catalog.Models))
	for _, m := range catalog.Models {
		providerID := sfProviderID(m.ProviderID)
		if providerID == "" {
			continue
		}
		if !sfModelAllowed(providerID, m.ID) {
			continue
		}
		identity := Normalize(providerID, m.ID, m.Name)
		paramBillions := identity.ParamBillions
		if paramBillions == nil {
			paramBillions = m.ParamBillions
		}
		sizeClass := identity.SizeClass
		if sizeClass == "" {
			sizeClass = m.SizeClass
		}
		models = append(models, SFModelOverlay{
			ProviderID:    providerID,
			WireModelID:   m.ID,
			CanonicalSlug: identity.CanonicalSlug,
			Family:        identity.Family,
			Version:       identity.Version,
			SizeClass:     sizeClass,
			ParamBillions: paramBillions,
			CanReason:     m.CanReason,
			ContextWindow: m.ContextWindow,
		})
	}
	return SFExport{
		GeneratedAt: catalog.GeneratedAt,
		Models:      models,
		Policy: SFPolicy{
			JudgmentMinimumSizeClass: "standard",
			TinySizeMaxBillions:      8,
			SmallSizeMaxBillions:     14,
			ProviderModelAllow: map[string][]string{
				"openrouter": {":free"},
				"minimax":    {"MiniMax-M2.7", "MiniMax-M2.7-highspeed"},
			},
			PreferredProvidersByFamily: map[string][]string{
				"kimi":    {"kimi-coding", "ollama-cloud", "opencode-go"},
				"minimax": {"minimax", "opencode-go"},
			},
		},
	}
}

func sfProviderID(providerID string) string {
	id := strings.ToLower(strings.TrimSpace(providerID))
	switch id {
	case "kimi-for-coding":
		return "kimi-coding"
	case "minimax-cn", "minimax-china", "minimax-cn-coding-plan":
		return ""
	case "minimax-coding-plan":
		return "minimax"
	default:
		return id
	}
}

func sfModelAllowed(providerID, modelID string) bool {
	if strings.EqualFold(providerID, "openrouter") {
		return strings.HasSuffix(strings.ToLower(strings.TrimSpace(modelID)), ":free")
	}
	if strings.EqualFold(providerID, "minimax") {
		switch strings.TrimSpace(modelID) {
		case "MiniMax-M2.7", "MiniMax-M2.7-highspeed":
			return true
		default:
			return false
		}
	}
	return true
}

func cloneCatalog(c Catalog) Catalog {
	out := c
	out.Sources = append([]SourceStatus(nil), c.Sources...)
	out.Providers = append([]Provider(nil), c.Providers...)
	out.Models = append([]Model(nil), c.Models...)
	return out
}
