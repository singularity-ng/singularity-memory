package modelcatalog

import "testing"

func TestNormalizeParsesCommonSizeTags(t *testing.T) {
	cases := []struct {
		id        string
		sizeClass string
		params    float64
	}{
		{id: "qwen3:8b", sizeClass: "tiny", params: 8},
		{id: "gemma3:12b", sizeClass: "small", params: 12},
		{id: "mistralai/ministral-3b-2512", sizeClass: "tiny", params: 3},
		{id: "mistral.ministral-3-8b-instruct", sizeClass: "tiny", params: 8},
		{id: "qwen3-coder:30b", sizeClass: "standard", params: 30},
		{id: "llama-3.1-405b", sizeClass: "frontier", params: 405},
	}
	for _, tc := range cases {
		got := Normalize("test", tc.id, "")
		if got.SizeClass != tc.sizeClass {
			t.Fatalf("%s size class = %q, want %q", tc.id, got.SizeClass, tc.sizeClass)
		}
		if got.ParamBillions == nil || *got.ParamBillions != tc.params {
			t.Fatalf("%s params = %v, want %v", tc.id, got.ParamBillions, tc.params)
		}
	}
}
