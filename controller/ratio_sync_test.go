package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
)

func TestPricingSyncExpressionPriority(t *testing.T) {
	expression := `tier("base", p * 2 + c * 8 + cr * 0)`
	cases := []struct {
		name       string
		local      map[string]any
		source     map[string]any
		wantFields []string
	}{
		{"equal expressions suppress stale ratios", map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}, "model_price": map[string]float64{"m": 2}}, nil},
		{"local expression excludes legacy-only source", map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}}, map[string]any{"model_ratio": map[string]float64{"m": 3}, "completion_ratio": map[string]float64{"m": 2}}, nil},
		{"expression imports without legacy conflicts", map[string]any{"model_ratio": map[string]float64{"m": 1}}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}, "model_price": map[string]float64{"m": 2}}, []string{"billing_mode", "billing_expr"}},
		{"inactive expression follows explicit ratio mode", map[string]any{"model_ratio": map[string]float64{"m": 1}}, map[string]any{"billing_mode": map[string]string{"m": "ratio"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}}, []string{"model_ratio"}},
		{"empty active expression never imports a false free price", map[string]any{}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": " "}, "model_ratio": map[string]float64{"m": 0}}, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			diff := buildDifferences(tt.local, []struct {
				name string
				data map[string]any
			}{{"source", tt.source}})
			fields := make([]string, 0, len(diff["m"]))
			for field := range diff["m"] {
				fields = append(fields, field)
			}
			assert.ElementsMatch(t, tt.wantFields, fields)
		})
	}
}

func TestRatioConfigExportsEffectiveExpressions(t *testing.T) {
	before := config.GlobalConfig.ExportAllConfigs()
	expose := ratio_setting.IsExposeRatioEnabled()
	t.Cleanup(func() {
		config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": before["billing_setting.billing_mode"], "billing_expr": before["billing_setting.billing_expr"]})
		ratio_setting.SetExposeRatioEnabled(expose)
	})
	config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": `{"sync-export":"tiered_expr"}`, "billing_expr": `{"sync-export":"tier(\"base\", p * 2)"}`})
	ratio_setting.SetExposeRatioEnabled(true)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/ratio_config", nil)
	GetRatioConfig(c)
	var response struct {
		Success bool
		Data    map[string]any
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, billing_setting.BillingModeTieredExpr, valueMap(response.Data["billing_mode"])["sync-export"])
	assert.Equal(t, `tier("base", p * 2)`, valueMap(response.Data["billing_expr"])["sync-export"])
}

func TestPricingSyncCompleteSourcesAndArrayFormats(t *testing.T) {
	before := config.GlobalConfig.ExportAllConfigs()
	oldRatios, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	t.Cleanup(func() {
		config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": before["billing_setting.billing_mode"], "billing_expr": before["billing_setting.billing_expr"]})
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"sync-token":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"sync-token":2}`))
	expression := `len <= 200000 ? tier("short", p * 2 + c * 8 + cr * 0) : tier("long", p * 4 + c * 12)`
	expressions, err := common.Marshal(map[string]string{"sync-already": expression})
	require.NoError(t, err)
	config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": `{"sync-already":"tiered_expr"}`, "billing_expr": string(expressions)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		if r.URL.Path == "/ratio_config" {
			data = map[string]any{
				"billing_mode":     map[string]string{"sync-already": "tiered_expr", "sync-expression": "tiered_expr"},
				"billing_expr":     map[string]string{"sync-already": expression, "sync-expression": expression},
				"model_ratio":      map[string]float64{"sync-already": 9, "sync-expression": 9, "sync-token": 1},
				"model_price":      map[string]float64{"sync-expression": 4},
				"completion_ratio": map[string]float64{"sync-token": 4},
				"cache_ratio":      map[string]float64{"sync-token": 0},
			}
		} else {
			data = []map[string]any{
				{"model_name": "sync-already", "model_ratio": 5, "completion_ratio": 3},
				{"model_name": "sync-expression", "model_ratio": 2, "model_price": 1},
				{"model_name": "sync-array-expression", "billing_mode": "tiered_expr", "billing_expr": expression, "quota_type": 1, "model_price": 0},
				{"model_name": "sync-unpriced"},
				{"model_name": "sync-invalid-expression", "billing_mode": "tiered_expr", "billing_expr": "", "model_ratio": 0},
				{"model_name": "sync-free", "model_ratio": 0, "completion_ratio": 0},
			}
		}
		encoded, err := common.Marshal(map[string]any{"success": true, "data": data})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	defer server.Close()
	var response struct {
		Success bool
		Data    struct {
			Differences map[string]map[string]dto.DifferenceItem
			Prices      map[string]struct {
				Current   map[string]any
				Upstreams map[string]map[string]any
			}
			TestResults []dto.TestResult `json:"test_results"`
		}
	}
	body := map[string]any{"upstreams": []map[string]any{
		{"id": 1, "name": "Expressions", "base_url": server.URL, "endpoint": "/ratio_config"},
		{"id": 2, "name": "Legacy", "base_url": server.URL, "endpoint": "/pricing"},
	}}
	recorder := modelManagementRequest(t, FetchUpstreamRatios, http.MethodPost, "/api/channel/fetch_upstream_ratios", body, &response)
	require.True(t, response.Success, recorder.Body.String())
	require.Len(t, response.Data.TestResults, 2)
	for _, result := range response.Data.TestResults {
		require.Equal(t, "success", result.Status, result.Error)
	}
	assert.NotContains(t, response.Data.Differences, "sync-already")
	assert.NotContains(t, response.Data.Differences, "sync-unpriced")
	assert.NotContains(t, response.Data.Differences, "sync-invalid-expression")
	assert.Equal(t, map[string]any{"billing_mode": "tiered_expr", "billing_expr": expression}, response.Data.Prices["sync-expression"].Upstreams["Expressions(1)"])
	assert.NotContains(t, response.Data.Prices["sync-expression"].Upstreams, "Legacy(2)")
	assert.Equal(t, map[string]any{"billing_mode": "tiered_expr", "billing_expr": expression}, response.Data.Prices["sync-array-expression"].Upstreams["Legacy(2)"])
	assert.Equal(t, float64(1), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["model_ratio"], "unchanged base prices are included for a complete price preview")
	assert.Equal(t, float64(4), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["completion_ratio"])
	assert.Equal(t, float64(0), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["cache_ratio"])
	assert.Equal(t, float64(0), response.Data.Prices["sync-free"].Upstreams["Legacy(2)"]["model_ratio"])
	assert.NotContains(t, response.Data.Prices, "sync-already")
	body["include_unchanged"] = true
	recorder = modelManagementRequest(t, FetchUpstreamRatios, http.MethodPost, "/api/ratio_sync/fetch", body, &response)
	require.True(t, response.Success, recorder.Body.String())
	assert.Equal(t, map[string]any{"billing_mode": "tiered_expr", "billing_expr": expression}, response.Data.Prices["sync-already"].Upstreams["Expressions(1)"])
	assert.NotContains(t, response.Data.Prices["sync-already"].Upstreams, "Legacy(2)")
	assert.NotContains(t, response.Data.Prices, "sync-unpriced")
	assert.NotContains(t, response.Data.Prices, "sync-invalid-expression")
	assert.NotContains(t, response.Data.Differences, "sync-already")
}

func TestModelsDevPricingSuggestion(t *testing.T) {
	var catalog modelsDevCatalog
	require.NoError(t, common.UnmarshalJsonStr(`{
		"openrouter": {"models": {"claude-haiku-4-5": {"cost": {"input": 0.5, "output": 2}}}},
		"abacus": {"models": {"claude-3-7-sonnet-20250219": {"cost": {"input": 3, "output": 15}}}},
		"anthropic": {"models": {
			"claude-haiku-4-5": {"cost": {"input": 1, "output": 5, "cache_read": 0.1, "cache_write": 1.25}},
			"claude-haiku-5-5": {"cost": {"input": 0.1, "output": 0.5, "cache_read": 0.01, "tiers": [{"input": 0.5, "output": 2.5, "cache_read": 0.05, "tier": {"type": "context", "size": 100000}}]}}
		}},
		"amazon-bedrock": {"models": {"anthropic.claude-opus-4-1-20250805-v1:0": {"cost": {"input": 15, "output": 75, "cache_read": 1.5, "cache_write": 18.75}}}},
		"openai": {"models": {
			"gpt-6-astra": {"cost": {"input": 10, "output": 50, "cache_read": 1, "cache_write": 12.5, "tiers": [{"input": 20, "output": 75, "cache_read": 2, "cache_write": 25, "tier": {"type": "context", "size": 272001}}]}},
			"gpt-4o": {"cost": {"input": 2.5, "output": 10, "cache_read": 1.25}},
			"gpt-bad": {"cost": {"input": -1, "output": 10}}
		}}
	}`, &catalog))

	cases := []struct {
		name        string
		model       string
		provider    string
		sourceModel string
		expression  string
		derived     []string
	}{
		{"official provider wins over cheaper reseller", "claude-haiku-4-5", "anthropic", "claude-haiku-4-5",
			`tier("standard", p * 1 + c * 5 + cr * 0.1 + cc * 1.25 + cc1h * 2)`, []string{"cc1h"}},
		{"dated id falls back to the date-less entry", "claude-haiku-4-5-20251001", "anthropic", "claude-haiku-4-5",
			`tier("standard", p * 1 + c * 5 + cr * 0.1 + cc * 1.25 + cc1h * 2)`, []string{"cc1h"}},
		{"claude derives a missing 5m write for every tier", "claude-haiku-5-5", "anthropic", "claude-haiku-5-5",
			`len <= 100000 ? tier("standard", p * 0.1 + c * 0.5 + cr * 0.01 + cc * 0.125 + cc1h * 0.2) : tier("long_context", p * 0.5 + c * 2.5 + cr * 0.05 + cc * 0.625 + cc1h * 1)`, []string{"cc", "cc1h"}},
		{"bedrock naming matches a third-party provider", "us.anthropic.claude-opus-4-1-20250805-v1:0", "amazon-bedrock", "anthropic.claude-opus-4-1-20250805-v1:0",
			`tier("standard", p * 15 + c * 75 + cr * 1.5 + cc * 18.75 + cc1h * 30)`, []string{"cc1h"}},
		{"reseller without cache prices gets Anthropic cache multiples", "claude-3-7-sonnet-20250219", "abacus", "claude-3-7-sonnet-20250219",
			`tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`, []string{"cc", "cc1h", "cr"}},
		{"gpt long context uses the normalized tier boundary", "gpt-6-astra", "openai", "gpt-6-astra",
			`len <= 272000 ? tier("standard", p * 10 + c * 50 + cr * 1 + cc * 12.5 + cc12h * 25) : tier("long_context", p * 20 + c * 75 + cr * 2 + cc * 25 + cc12h * 50)`, []string{"cc12h"}},
		{"gpt without cache write prices no cache creation", "gpt-4o", "openai", "gpt-4o",
			`tier("standard", p * 2.5 + c * 10 + cr * 1.25)`, []string{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			suggestion, err := buildModelsDevPricingSuggestion(catalog, tt.model)
			require.NoError(t, err)
			assert.Equal(t, tt.provider, suggestion.Provider)
			assert.Equal(t, tt.sourceModel, suggestion.SourceModel)
			assert.Equal(t, tt.expression, suggestion.Expression)
			assert.Equal(t, tt.derived, suggestion.DerivedFields)
		})
	}

	_, err := buildModelsDevPricingSuggestion(catalog, "unknown-model")
	assert.EqualError(t, err, "Model not found on models.dev")
	_, err = buildModelsDevPricingSuggestion(catalog, "gpt-bad")
	assert.EqualError(t, err, "models.dev pricing for this model is invalid")
}
