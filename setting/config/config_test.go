package config

import (
	"testing"
)

type testConfigWithMap struct {
	Modes map[string]string `json:"modes"`
	Exprs map[string]string `json:"exprs"`
	Name  string            `json:"name"`
}

func TestUpdateConfigFromMap_MapReplacement(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{
			"model-a": "tiered_expr",
			"model-b": "tiered_expr",
		},
		Exprs: map[string]string{
			"model-a": "p * 5 + c * 25",
			"model-b": "p * 10 + c * 50",
		},
		Name: "billing",
	}

	// Simulate removing model-a: new value only has model-b
	err := UpdateConfigFromMap(cfg, map[string]string{
		"modes": `{"model-b": "tiered_expr"}`,
		"exprs": `{"model-b": "p * 10 + c * 50"}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if _, ok := cfg.Modes["model-a"]; ok {
		t.Errorf("Modes still contains model-a after it was removed from the update; got %v", cfg.Modes)
	}
	if _, ok := cfg.Exprs["model-a"]; ok {
		t.Errorf("Exprs still contains model-a after it was removed from the update; got %v", cfg.Exprs)
	}

	if cfg.Modes["model-b"] != "tiered_expr" {
		t.Errorf("Modes[model-b] = %q, want %q", cfg.Modes["model-b"], "tiered_expr")
	}
	if cfg.Exprs["model-b"] != "p * 10 + c * 50" {
		t.Errorf("Exprs[model-b] = %q, want %q", cfg.Exprs["model-b"], "p * 10 + c * 50")
	}
}

func TestUpdateConfigFromMap_EmptyMapClearsAll(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{
			"model-a": "tiered_expr",
		},
		Exprs: map[string]string{
			"model-a": "p * 5 + c * 25",
		},
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"modes": `{}`,
		"exprs": `{}`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if len(cfg.Modes) != 0 {
		t.Errorf("Modes should be empty after updating with {}, got %v", cfg.Modes)
	}
	if len(cfg.Exprs) != 0 {
		t.Errorf("Exprs should be empty after updating with {}, got %v", cfg.Exprs)
	}
}

type testProxyEntry struct {
	Alias     string `json:"alias"`
	URL       string `json:"url"`
	ProxyType string `json:"proxy_type,omitempty"`
}

type testConfigWithSliceOfStruct struct {
	Proxies []testProxyEntry `json:"proxies"`
}

// TestUpdateConfigFromMap_SliceOmitemptyFieldCleared guards against a bug where
// switching a proxy-pool entry (or any []struct field with omitempty members)
// from "tp" back to the default "normal" type did not take effect without a
// process restart: json.Unmarshal into an already-populated slice reuses the
// existing elements and leaves fields absent from the new JSON (like an
// omitempty ProxyType) at their old value instead of clearing them.
func TestUpdateConfigFromMap_SliceOmitemptyFieldCleared(t *testing.T) {
	cfg := &testConfigWithSliceOfStruct{
		Proxies: []testProxyEntry{
			{Alias: "gost-1", URL: "http://10.0.0.1:1080", ProxyType: "tp"},
		},
	}

	// Simulate saving the entry back as "normal" (proxy_type omitted).
	err := UpdateConfigFromMap(cfg, map[string]string{
		"proxies": `[{"alias":"gost-1","url":"http://10.0.0.1:1080"}]`,
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if got := cfg.Proxies[0].ProxyType; got != "" {
		t.Errorf("ProxyType = %q, want empty (normal) after it was omitted from the update", got)
	}
}

func TestUpdateConfigFromMap_ScalarFieldsUnchanged(t *testing.T) {
	cfg := &testConfigWithMap{
		Modes: map[string]string{"m": "v"},
		Name:  "old",
	}

	err := UpdateConfigFromMap(cfg, map[string]string{
		"name": "new",
	})
	if err != nil {
		t.Fatalf("UpdateConfigFromMap failed: %v", err)
	}

	if cfg.Name != "new" {
		t.Errorf("Name = %q, want %q", cfg.Name, "new")
	}
	// modes was not in configMap, should remain unchanged
	if cfg.Modes["m"] != "v" {
		t.Errorf("Modes should be unchanged, got %v", cfg.Modes)
	}
}
