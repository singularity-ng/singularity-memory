package modelcatalog

import (
	"slices"
	"testing"
	"time"
)

func TestSFExportFiltersOpenRouterToFreeModels(t *testing.T) {
	service := NewService("", Fetcher{})
	service.catalog = Catalog{
		GeneratedAt: time.Now().UTC(),
		Models: []Model{
			{ProviderID: "openrouter", ID: "qwen/qwen3-coder:free", Name: "Qwen3 Coder Free"},
			{ProviderID: "openrouter", ID: "qwen/qwen3-coder", Name: "Qwen3 Coder Paid"},
		},
	}

	export := service.SFExport()
	if len(export.Models) != 1 {
		t.Fatalf("exported model count = %d: %+v", len(export.Models), export.Models)
	}
	if export.Models[0].ProviderID != "openrouter" || export.Models[0].WireModelID != "qwen/qwen3-coder:free" {
		t.Fatalf("unexpected exported model: %+v", export.Models[0])
	}
	if !slices.Contains(export.Policy.ProviderModelAllow["openrouter"], ":free") {
		t.Fatalf("missing OpenRouter :free policy: %+v", export.Policy.ProviderModelAllow)
	}
}

func TestSFExportCanonicalizesProviderIDs(t *testing.T) {
	service := NewService("", Fetcher{})
	service.catalog = Catalog{
		GeneratedAt: time.Now().UTC(),
		Models: []Model{
			{ProviderID: "KIMI-FOR-CODING", ID: "kimi-for-coding", Name: "Kimi K2.6"},
			{ProviderID: "minimax-china", ID: "MiniMax-M2.7", Name: "MiniMax M2.7 CN"},
			{ProviderID: "minimax-coding-plan", ID: "MiniMax-M2.7-highspeed", Name: "MiniMax M2.7 Highspeed"},
			{ProviderID: "minimax-cn-coding-plan", ID: "MiniMax-M2.7", Name: "MiniMax M2.7 CN Coding"},
			{ProviderID: "minimax", ID: "MiniMax-M2.5", Name: "MiniMax M2.5"},
		},
	}

	export := service.SFExport()
	got := map[string]bool{}
	for _, model := range export.Models {
		got[model.ProviderID] = true
		if slices.Contains([]string{"minimax-china", "minimax-cn", "minimax-cn-coding-plan", "kimi-for-coding"}, model.ProviderID) {
			t.Fatalf("uncanonical provider leaked into SF export: %+v", model)
		}
		if model.ProviderID == "minimax" && model.WireModelID == "MiniMax-M2.5" {
			t.Fatalf("disallowed MiniMax model leaked into SF export: %+v", model)
		}
	}
	for _, providerID := range []string{"kimi-coding", "minimax"} {
		if !got[providerID] {
			t.Fatalf("missing canonical provider %q in %+v", providerID, export.Models)
		}
	}
	for _, allowedModelID := range []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed"} {
		if !slices.Contains(export.Policy.ProviderModelAllow["minimax"], allowedModelID) {
			t.Fatalf("missing MiniMax allow policy %q in %+v", allowedModelID, export.Policy.ProviderModelAllow)
		}
	}
}
