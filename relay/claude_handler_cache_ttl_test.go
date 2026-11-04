package relay

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// deleteClaudeCacheBreakpointStateForTest removes a key from the cache
// backing claude_cache_breakpoint_state.go, so tests that write a breakpoint
// state under t.Name() do not leak between runs or pollute other tests.
func deleteClaudeCacheBreakpointStateForTest(t *testing.T, key string) {
	t.Helper()
	_, err := getClaudeCacheBreakpointStatesCache().DeleteMany([]string{key})
	require.NoError(t, err)
}

func TestRetainedClaudePrefixIdentity(t *testing.T) {
	const body = `{"model":"claude","messages":[{"role":"user","content":[{"type":"text","text":"first"},{"type":"text","text":"anchor","cache_control":{"type":"ephemeral"}}]}]}`
	previous := buildClaudeCacheSkeleton([]byte(body))
	current := strings.ReplaceAll(body, `,"cache_control":{"type":"ephemeral"}`, "")
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"unchanged", current, true},
		{"earlier block changed", strings.Replace(current, `"first"`, `"changed"`, 1), false},
		{"role changed", strings.Replace(current, `"user"`, `"assistant"`, 1), false},
		{"parameter changed", strings.Replace(current, `"model":"claude"`, `"model":"other"`, 1), false},
		{"message boundary changed", `{"model":"claude","messages":[{"role":"user","content":[{"type":"text","text":"first"}]},{"role":"user","content":[{"type":"text","text":"anchor"}]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := retainClaudeCacheBreakpoints([]byte(tc.body), &claudeCacheBreakpointState{
				ParamFp: previous.ParamFp, BlkHash: previous.BlkHash, BlkKind: previous.BlkKind,
				MsgStart: previous.MsgStart, BpIdx: previous.BpIdx, BpTtl: previous.BpTtl,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, changed)
			if !tc.want {
				assert.JSONEq(t, tc.body, string(out))
			}
		})
	}
}

func TestResolveClaudeCacheOptimization(t *testing.T) {
	t.Setenv("CACHE_SKELETON_ENABLED", "false")
	setting := operation_setting.GetExtendedCacheTTLSetting()
	previousRules := setting.Rules
	setting.Rules = []operation_setting.GroupModelRule{{Group: "default", Models: []string{"claude-sonnet"}}}
	t.Cleanup(func() { setting.Rules = previousRules })
	require.True(t, operation_setting.ShouldForceExtendedCacheTTL("default", "claude-sonnet"))
	t.Run("nil inherits legacy", func(t *testing.T) {
		for _, name := range []string{"claude-sonnet", "alias"} {
			got := resolveClaudeCacheOptimization(nil, "default", name)
			assert.Equal(t, operation_setting.ShouldForceExtendedCacheTTL("default", name), got.ForceExtendedTTL.Enabled)
			assert.Equal(t, operation_setting.ShouldFixCacheBreakpoints("default", name), got.FixVolatileBreakpoint.Enabled)
			assert.Equal(t, operation_setting.ShouldUseStableBaseBreakpoint("default", name), got.StableBaseBreakpoint.Enabled)
			assert.Equal(t, operation_setting.ShouldRetainPreviousBreakpoint("default", name), got.RetainPreviousBreakpoint.Enabled)
			assert.Equal(t, operation_setting.ShouldUseFallbackCacheBreakpoint("default", name), got.FallbackCacheBreakpoint.Enabled)
		}
	})
	t.Run("global rules are independent, not coupled through fix or diagnostics", func(t *testing.T) {
		fixSetting := operation_setting.GetCacheBreakpointFixSetting()
		stableSetting := operation_setting.GetStableBaseBreakpointSetting()
		retainSetting := operation_setting.GetRetainPreviousBreakpointSetting()
		fallbackSetting := operation_setting.GetFallbackCacheBreakpointSetting()
		t.Cleanup(func() {
			fixSetting.Rules, stableSetting.Rules = nil, nil
			retainSetting.Rules, fallbackSetting.Rules = nil, nil
		})
		// Enable only FixVolatileBreakpoint globally; the rest must stay off.
		fixSetting.Rules = []operation_setting.GroupModelRule{{Group: "default", Models: []string{"*"}}}
		got := resolveClaudeCacheOptimization(nil, "default", "claude-sonnet")
		assert.True(t, got.FixVolatileBreakpoint.Enabled)
		assert.False(t, got.StableBaseBreakpoint.Enabled, "stable base must not inherit the fix-volatile-breakpoint setting")
		assert.False(t, got.RetainPreviousBreakpoint.Enabled)
		assert.False(t, got.FallbackCacheBreakpoint.Enabled)

		// Enabling RetainPreviousBreakpoint globally must not depend on
		// CacheSkeletonEnabled(), which this test forces off via the env var.
		require.False(t, model.CacheSkeletonEnabled())
		retainSetting.Rules = []operation_setting.GroupModelRule{{Group: "default", Models: []string{"*"}}}
		got = resolveClaudeCacheOptimization(nil, "default", "claude-sonnet")
		assert.True(t, got.RetainPreviousBreakpoint.Enabled)

		// StableBaseBreakpoint and FallbackCacheBreakpoint each have their own
		// independent global knob.
		stableSetting.Rules = []operation_setting.GroupModelRule{{Group: "default", Models: []string{"*"}}}
		fallbackSetting.Rules = []operation_setting.GroupModelRule{{Group: "default", Models: []string{"*"}}}
		got = resolveClaudeCacheOptimization(nil, "default", "claude-sonnet")
		assert.True(t, got.StableBaseBreakpoint.Enabled)
		assert.True(t, got.FallbackCacheBreakpoint.Enabled)
	})
	t.Run("explicit all off", func(t *testing.T) {
		assert.Equal(t, model.CustomerClaudeCacheOptimization{},
			resolveClaudeCacheOptimization(&model.CustomerClaudeCacheOptimization{}, "default", "claude-sonnet"))
	})
	t.Run("independent prefixes and empty matches all", func(t *testing.T) {
		config := &model.CustomerClaudeCacheOptimization{
			ForceExtendedTTL:         model.CustomerClaudeCacheOptimizationRule{Enabled: true, ModelPrefixes: []string{"claude-sonnet"}},
			FixVolatileBreakpoint:    model.CustomerClaudeCacheOptimizationRule{Enabled: true, ModelPrefixes: []string{"claude-opus"}},
			StableBaseBreakpoint:     model.CustomerClaudeCacheOptimizationRule{Enabled: true},
			RetainPreviousBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: true, ModelPrefixes: []string{"alias"}},
			FallbackCacheBreakpoint:  model.CustomerClaudeCacheOptimizationRule{Enabled: true, ModelPrefixes: []string{"claude-sonnet"}},
		}
		for _, tc := range []struct {
			name             string
			ttl, fix, retain bool
		}{
			{"claude-sonnet-4", true, false, false},
			{"claude-opus-4", false, true, false},
			{"alias-v1", false, false, true},
			{"other", false, false, false},
		} {
			got := resolveClaudeCacheOptimization(config, "default", tc.name)
			assert.Equal(t, tc.ttl, got.ForceExtendedTTL.Enabled, tc.name)
			assert.Equal(t, tc.fix, got.FixVolatileBreakpoint.Enabled, tc.name)
			assert.Equal(t, tc.retain, got.RetainPreviousBreakpoint.Enabled, tc.name)
			assert.True(t, got.StableBaseBreakpoint.Enabled, tc.name)
			assert.Equal(t, tc.ttl, got.FallbackCacheBreakpoint.Enabled, tc.name)
		}
	})
	t.Run("fallback alone with empty prefixes", func(t *testing.T) {
		config := &model.CustomerClaudeCacheOptimization{
			FallbackCacheBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: true},
		}
		got := resolveClaudeCacheOptimization(config, "default", "custom-alias")
		assert.True(t, got.FallbackCacheBreakpoint.Enabled)
		assert.False(t, got.StableBaseBreakpoint.Enabled)
		assert.False(t, got.ForceExtendedTTL.Enabled)
		config.FallbackCacheBreakpoint.Enabled = false
		assert.False(t, resolveClaudeCacheOptimization(config, "default", "custom-alias").FallbackCacheBreakpoint.Enabled)
	})
}

// TestClaudeCacheOptimizationConfigured covers the pass-through-mode
// diagnostic marker: pass-through skips all five cache-optimization rules
// silently, so a customer's configured policy must only be flagged as skipped
// when at least one rule would otherwise have run.
func TestClaudeCacheOptimizationConfigured(t *testing.T) {
	assert.False(t, claudeCacheOptimizationConfigured(model.CustomerClaudeCacheOptimization{}))
	for _, tc := range []struct {
		name   string
		policy model.CustomerClaudeCacheOptimization
	}{
		{"force extended ttl", model.CustomerClaudeCacheOptimization{ForceExtendedTTL: model.CustomerClaudeCacheOptimizationRule{Enabled: true}}},
		{"fix volatile breakpoint", model.CustomerClaudeCacheOptimization{FixVolatileBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: true}}},
		{"stable base breakpoint", model.CustomerClaudeCacheOptimization{StableBaseBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: true}}},
		{"retain previous breakpoint", model.CustomerClaudeCacheOptimization{RetainPreviousBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: true}}},
		{"fallback cache breakpoint", model.CustomerClaudeCacheOptimization{FallbackCacheBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, claudeCacheOptimizationConfigured(tc.policy))
		})
	}
}

func TestClaudeFallbackCacheBreakpoint(t *testing.T) {
	for _, tc := range []struct {
		name, body, path string
	}{
		{"message text", `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`, "messages.0.content.0"},
		{"plain string", `{"messages":[{"role":"user","content":"hello"}]}`, "messages.0.content.0"},
		{"skip thinking and empty text", `{"messages":[{"role":"assistant","content":[{"type":"text","text":"hello"},{"type":"thinking","thinking":"reason"},{"type":"redacted_thinking","data":"opaque"},{"type":"text","text":""}]}]}`, "messages.0.content.0"},
		{"skip turn scoped message", `{"messages":[{"role":"user","content":"hello"},{"role":"system","clear_at":"next_user_message","content":[{"type":"text","text":"temporary"}]}]}`, "messages.0.content.0"},
		{"persistent clear at", `{"messages":[{"role":"system","clear_at":"never","content":"hello"}]}`, "messages.0.content.0"},
		{"system fallback", `{"system":"foundation","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason"}]}]}`, "system.0"},
		{"tools fallback", `{"tools":[{"name":"first"},{"name":"last"}],"messages":[]}`, "tools.1"},
		{"business field is not a cache marker", `{"tools":[{"name":"t","input_schema":{"type":"object","properties":{"cache_control":{"type":"string"}}}}],"messages":[{"role":"user","content":"hello"}]}`, "messages.0.content.0"},
		{"automatic cache", `{"cache_control":{"type":"ephemeral"},"messages":[{"role":"user","content":"hello"}]}`, ""},
		{"explicit system cache", `{"system":[{"type":"text","text":"base","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hello"}]}`, ""},
		{"explicit tools cache", `{"tools":[{"name":"t","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hello"}]}`, ""},
		{"explicit message cache", `{"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}]}`, ""},
		{"null marker retained", `{"cache_control":null,"messages":[{"role":"user","content":"hello"}]}`, ""},
		{"malformed block marker retained", `{"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":false}]}]}`, ""},
		{"no legal target", `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason"},{"type":"text","text":""}]}]}`, ""},
		{"empty body", `{}`, ""},
		{"invalid message shape retained", `{"messages":{"user":{"content":[{"type":"text","text":"hello"}]}}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, added, err := addClaudeFallbackCacheBreakpoint([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, tc.path != "", added)
			if tc.path == "" {
				assert.Equal(t, tc.body, string(out), "existing client configuration is untouched")
				return
			}
			assert.Equal(t, "ephemeral", gjson.GetBytes(out, tc.path+".cache_control.type").String())
			assert.Equal(t, "5m", gjson.GetBytes(out, tc.path+".cache_control.ttl").String())
			assert.Len(t, claudeCacheControlPaths(out), 1)
			if text := gjson.Get(tc.body, "messages.0.content"); text.Type == gjson.String {
				assert.Equal(t, text.String(), gjson.GetBytes(out, "messages.0.content.0.text").String())
			}
			again, addedAgain, err := addClaudeFallbackCacheBreakpoint(out)
			require.NoError(t, err)
			assert.False(t, addedAgain)
			assert.Equal(t, string(out), string(again))
			extended, err := forceClaudeCacheControlTTL(out, "1h")
			require.NoError(t, err)
			assert.Equal(t, "1h", gjson.GetBytes(extended, tc.path+".cache_control.ttl").String())
		})
	}
}

// TestClaudeFallbackCacheBreakpointSkipsVolatileTail covers a prior gap: the
// fallback path picked the last cacheable block without checking whether it
// was a known per-turn-changing reminder, unlike the stable-base and
// fix-volatile-breakpoint paths. A breakpoint placed there would be a cache
// write on every turn and never a read.
func TestClaudeFallbackCacheBreakpointSkipsVolatileTail(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"stable question"},{"type":"text","text":"<total_tokens>9 left</total_tokens>"}]}]}`)
	out, added, err := addClaudeFallbackCacheBreakpoint(body)
	require.NoError(t, err)
	require.True(t, added)
	assert.Equal(t, "ephemeral", gjson.GetBytes(out, "messages.0.content.0.cache_control.type").String(),
		"breakpoint should land on the stable block, not the volatile reminder")
	assert.False(t, gjson.GetBytes(out, "messages.0.content.1.cache_control").Exists())
}

// TestClaudeVolatileTailPatternsConfigurable covers the admin override of
// claude_volatile_tail_pattern_setting: a custom marker is recognized, and an
// empty override falls back to the built-in default.
func TestClaudeVolatileTailPatternsConfigurable(t *testing.T) {
	setting, ok := config.GlobalConfig.Get("claude_volatile_tail_pattern_setting").(*operation_setting.ClaudeVolatileTailPatternSetting)
	require.True(t, ok)
	t.Cleanup(func() { *setting = operation_setting.ClaudeVolatileTailPatternSetting{} })
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"stable question"},{"type":"text","text":"custom-reminder-123"}]}]}`)

	out, added, err := addClaudeFallbackCacheBreakpoint(body)
	require.NoError(t, err)
	require.True(t, added)
	assert.True(t, gjson.GetBytes(out, "messages.0.content.1.cache_control").Exists(),
		"without an override, the custom marker is not recognized as volatile")

	*setting = operation_setting.ClaudeVolatileTailPatternSetting{Patterns: []string{"custom-reminder-"}}
	out, added, err = addClaudeFallbackCacheBreakpoint(body)
	require.NoError(t, err)
	require.True(t, added)
	assert.Equal(t, "ephemeral", gjson.GetBytes(out, "messages.0.content.0.cache_control.type").String())
	assert.False(t, gjson.GetBytes(out, "messages.0.content.1.cache_control").Exists())
}

func TestClaudeRetentionWithoutDiagnostics(t *testing.T) {
	t.Setenv("CACHE_SKELETON_ENABLED", "false")
	require.False(t, model.CacheSkeletonEnabled())
	body := []byte(`{"model":"claude-sonnet","messages":[{"role":"user","content":[{"type":"text","text":"stable","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`)
	row := buildClaudeCacheSkeleton(body)
	row.Status, row.CacheCreation = "ok", 1
	key := t.Name()
	t.Cleanup(func() { deleteClaudeCacheBreakpointStateForTest(t, key) })
	rememberClaudeCacheBreakpointState(key, row)
	state := getClaudeCacheBreakpointState(key, time.Now())
	require.NotNil(t, state)
	current := []byte(`{"model":"claude-sonnet","messages":[{"role":"user","content":[{"type":"text","text":"stable"}]}]}`)
	out, changed, err := retainClaudeCacheBreakpoints(current, state)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Equal(t, "1h", gjson.GetBytes(out, "messages.0.content.0.cache_control.ttl").String())
}

func TestClaudeRetentionExpiresEachAnchorAndRejectsOlderSnapshot(t *testing.T) {
	key := t.Name()
	t.Cleanup(func() { deleteClaudeCacheBreakpointStateForTest(t, key) })
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"a","cache_control":{"type":"ephemeral","ttl":"1h"}},{"type":"text","text":"b","cache_control":{"type":"ephemeral","ttl":"5m"}}]}]}`)
	row := buildClaudeCacheSkeleton(body)
	row.Ts = time.Now().Add(-10 * time.Minute)
	row.Status = "ok"
	row.CacheCreation = 1
	rememberClaudeCacheBreakpointState(key, row)
	state := getClaudeCacheBreakpointState(key, time.Now())
	require.NotNil(t, state)
	assert.Equal(t, []uint16{0}, state.BpIdx)
	older := *row
	older.Ts = row.Ts.Add(-time.Minute)
	older.BlkHash = []uint64{99, 100}
	rememberClaudeCacheBreakpointState(key, &older)
	state = getClaudeCacheBreakpointState(key, time.Now())
	require.NotNil(t, state)
	assert.Equal(t, row.BlkHash[:1], state.BlkHash)
	assert.Equal(t, row.Ts, state.Ts)
}

func TestRetainClaudeCacheBreakpointsPrefersLatestAnchor(t *testing.T) {
	body := []byte(`{"model":"claude","system":[{"type":"text","text":"base","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"old","cache_control":{"type":"ephemeral"}},{"type":"text","text":"recent","cache_control":{"type":"ephemeral"}}]}]}`)
	row := buildClaudeCacheSkeleton(body)
	current := []byte(`{"model":"claude","cache_control":{"type":"ephemeral"},"system":[{"type":"text","text":"base","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"old"},{"type":"text","text":"recent"},{"type":"text","text":"new","cache_control":{"type":"ephemeral"}}]}]}`)
	out, changed, err := retainClaudeCacheBreakpoints(current, &claudeCacheBreakpointState{
		ParamFp: row.ParamFp, BlkHash: row.BlkHash, BlkKind: row.BlkKind,
		MsgStart: row.MsgStart, BpIdx: row.BpIdx, BpTtl: row.BpTtl,
	})
	require.NoError(t, err)
	require.True(t, changed)
	assert.True(t, gjson.GetBytes(out, "messages.0.content.1.cache_control").Exists())
	assert.False(t, gjson.GetBytes(out, "messages.0.content.0.cache_control").Exists())
	assert.Len(t, claudeCacheControlPaths(out), 3, "automatic cache reserves the fourth slot")
}

func TestFixClaudeCacheBreakpointsCountsAutomaticSlot(t *testing.T) {
	body := []byte(`{"cache_control":{"type":"ephemeral"},"system":[{"type":"text","text":"base","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"earlier","cache_control":{"type":"ephemeral"}}]},{"role":"assistant","content":[{"type":"text","text":"stable"}]},{"role":"system","content":[{"type":"text","text":"<total_tokens>123</total_tokens>","cache_control":{"type":"ephemeral"}}]}]}`)
	out, fix, err := fixClaudeCacheBreakpoints(body)
	require.NoError(t, err)
	assert.Equal(t, cacheBreakpointMoved, fix)
	assert.True(t, gjson.GetBytes(out, "messages.1.content.0.cache_control").Exists())
	assert.False(t, gjson.GetBytes(out, "messages.2.content.0.cache_control").Exists())
	assert.Len(t, claudeCacheControlPaths(out), 3)
}

func TestForceClaudeCacheControlTTL(t *testing.T) {
	t.Run("rewrites automatic trailing cache TTL", func(t *testing.T) {
		out, err := forceClaudeCacheControlTTL([]byte(`{"cache_control":{"type":"ephemeral","ttl":"5m"},"messages":[{"role":"user","content":"hi"}]}`), "1h")
		require.NoError(t, err)
		assert.Equal(t, "1h", gjson.GetBytes(out, "cache_control.ttl").String())
		assert.Equal(t, "ephemeral", gjson.GetBytes(out, "cache_control.type").String())
		assert.Empty(t, claudeCacheControlPaths(out))
	})
	t.Run("rewrites ttl on system, tools and message content breakpoints", func(t *testing.T) {
		body := `{
			"system":[{"type":"text","text":"S","cache_control":{"type":"ephemeral"}}],
			"tools":[{"name":"t","cache_control":{"type":"ephemeral","ttl":"5m"}}],
			"messages":[
				{"role":"user","content":[{"type":"text","text":"m","cache_control":{"type":"ephemeral"}}]},
				{"role":"assistant","content":"plain string, no breakpoint"}
			]
		}`
		out, err := forceClaudeCacheControlTTL([]byte(body), "1h")
		require.NoError(t, err)

		assert.Equal(t, "1h", gjson.GetBytes(out, "system.0.cache_control.ttl").String())
		assert.Equal(t, "ephemeral", gjson.GetBytes(out, "system.0.cache_control.type").String(), "type preserved")
		assert.Equal(t, "1h", gjson.GetBytes(out, "tools.0.cache_control.ttl").String(), "existing 5m overwritten")
		assert.Equal(t, "1h", gjson.GetBytes(out, "messages.0.content.0.cache_control.ttl").String())
	})

	t.Run("leaves bodies without cache_control untouched", func(t *testing.T) {
		body := `{"system":[{"type":"text","text":"S"}],"messages":[{"role":"user","content":"hi"}]}`
		out, err := forceClaudeCacheControlTTL([]byte(body), "1h")
		require.NoError(t, err)
		assert.JSONEq(t, body, string(out))
	})

	t.Run("string system does not panic", func(t *testing.T) {
		body := `{"system":"plain","messages":[]}`
		out, err := forceClaudeCacheControlTTL([]byte(body), "1h")
		require.NoError(t, err)
		assert.JSONEq(t, body, string(out))
	})
}

func TestMergeAnthropicBeta(t *testing.T) {
	const beta = "extended-cache-ttl-2025-04-11"

	assert.Equal(t, beta, mergeAnthropicBeta("", beta))
	assert.Equal(t, "a,b,"+beta, mergeAnthropicBeta("a,b", beta))
	// idempotent: already present
	assert.Equal(t, "a,"+beta, mergeAnthropicBeta("a,"+beta, beta))
}

func TestFixClaudeCacheBreakpoints(t *testing.T) {
	const prefix = `"system":[{"type":"text","text":"S","cache_control":{"type":"ephemeral"}}],"messages":[
		{"role":"user","content":"fix it"},
		{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"s"},{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},`
	reminder := `{"role":"system","content":[{"type":"text","text":"<total_tokens>9 left</total_tokens>","cache_control":{"type":"ephemeral","ttl":"1h"}}]}`
	toolResult := `{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}`
	cases := []struct {
		name     string
		body     string
		wantFix  string
		wantAt   string // path of the new breakpoint, "" when unchanged
		tailKept bool
		tailPath string
		wantTTL  string
	}{
		{
			name:     "volatile reminder gains a breakpoint on the tool_result",
			body:     `{` + prefix + toolResult + `,` + reminder + `]}`,
			wantFix:  cacheBreakpointAdded,
			wantAt:   "messages.2.content.0.cache_control",
			tailKept: true,
			tailPath: "messages.3.content.0.cache_control",
			wantTTL:  "1h",
		},
		{
			name:     "all four slots used moves the tail breakpoint",
			body:     `{"tools":[{"name":"a","cache_control":{"type":"ephemeral"}},{"name":"b","cache_control":{"type":"ephemeral"}}],` + prefix + toolResult + `,` + reminder + `]}`,
			wantFix:  cacheBreakpointMoved,
			wantAt:   "messages.2.content.0.cache_control",
			tailPath: "messages.3.content.0.cache_control",
			wantTTL:  "1h",
		},
		{
			name:     "skips thinking when the previous message has no stable block",
			body:     `{` + prefix + reminder + `]}`,
			wantFix:  cacheBreakpointAdded,
			wantAt:   "messages.1.content.1.cache_control",
			tailKept: true,
			tailPath: "messages.2.content.0.cache_control",
			wantTTL:  "1h",
		},
		{
			name:    "target already cached is left alone",
			body:    `{` + prefix + `{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok","cache_control":{"type":"ephemeral"}}]},` + reminder + `]}`,
			wantFix: "",
		},
		{
			name:     "turn-scoped tail breakpoint is always moved",
			body:     `{` + prefix + toolResult + `,{"role":"system","clear_at":"next_user_message","content":[{"type":"text","text":"note","cache_control":{"type":"ephemeral"}}]}]}`,
			wantFix:  cacheBreakpointMoved,
			wantAt:   "messages.2.content.0.cache_control",
			tailPath: "messages.3.content.0.cache_control",
		},
		{
			name:     "string content becomes a text block carrying the breakpoint",
			body:     `{"messages":[{"role":"user","content":"fix it"},` + reminder + `]}`,
			wantFix:  cacheBreakpointAdded,
			wantAt:   "messages.0.content.0.cache_control",
			tailKept: true,
			tailPath: "messages.1.content.0.cache_control",
			wantTTL:  "1h",
		},
		{
			name:    "breakpoint on a plain trailing user turn is not touched",
			body:    `{"messages":[{"role":"assistant","content":"a"},{"role":"user","content":[{"type":"text","text":"q","cache_control":{"type":"ephemeral"}}]}]}`,
			wantFix: "",
		},
		{
			name:    "stable system tail with full slots is not moved",
			body:    `{"tools":[{"name":"a","cache_control":{"type":"ephemeral"}},{"name":"b","cache_control":{"type":"ephemeral"}}],` + prefix + toolResult + `,{"role":"system","content":[{"type":"text","text":"# Environment","cache_control":{"type":"ephemeral"}}]}]}`,
			wantFix: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, fix, err := fixClaudeCacheBreakpoints([]byte(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.wantFix, fix)
			if tc.wantAt == "" {
				assert.JSONEq(t, tc.body, string(out))
				return
			}
			require.True(t, gjson.GetBytes(out, tc.wantAt).IsObject(), string(out))
			assert.Equal(t, tc.wantTTL, gjson.GetBytes(out, tc.wantAt+".ttl").String())
			assert.Equal(t, tc.tailKept, gjson.GetBytes(out, tc.tailPath).Exists())
			assert.LessOrEqual(t, len(claudeCacheControlPaths(out)), claudeMaxCacheBreakpoints)
		})
	}
}

func TestRetainClaudeCacheBreakpoints(t *testing.T) {
	body := `{"system":[{"type":"text","text":"stable"}],"messages":[
		{"role":"user","content":[{"type":"text","text":"old","cache_control":{"type":"ephemeral","ttl":"1h"}}]},
		{"role":"assistant","content":"answer"},
		{"role":"user","content":[{"type":"text","text":"new"}]}
	]}`
	previous := buildClaudeCacheSkeleton([]byte(body))
	previous.BlkHash = append([]uint64(nil), previous.BlkHash...)

	current := `{"system":[{"type":"text","text":"stable"}],"messages":[
		{"role":"user","content":[{"type":"text","text":"old"}]},
		{"role":"assistant","content":"answer"},
		{"role":"user","content":[{"type":"text","text":"new"}]}
	]}`
	out, changed, err := retainClaudeCacheBreakpoints([]byte(current), &claudeCacheBreakpointState{
		ParamFp: previous.ParamFp,
		BlkHash: previous.BlkHash,
		BpIdx:   previous.BpIdx,
		BpTtl:   previous.BpTtl,
	})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "1h", gjson.GetBytes(out, "messages.0.content.0.cache_control.ttl").String())
}

func TestRetainClaudeCacheBreakpointsSkipsChangedPrefix(t *testing.T) {
	previous := buildClaudeCacheSkeleton([]byte(`{"messages":[
		{"role":"user","content":[{"type":"text","text":"old","cache_control":{"type":"ephemeral"}}]}
	]}`))
	current := []byte(`{"messages":[
		{"role":"user","content":[{"type":"text","text":"changed"}]}
	]}`)
	out, changed, err := retainClaudeCacheBreakpoints(current, &claudeCacheBreakpointState{
		ParamFp: previous.ParamFp,
		BlkHash: previous.BlkHash,
		BpIdx:   previous.BpIdx,
		BpTtl:   previous.BpTtl,
	})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.JSONEq(t, string(current), string(out))
}

func TestPrepareClaudeTurnScopedMessages(t *testing.T) {
	body := `{"messages":[
		{"role":"user","content":"q"},
		{"role":"system","clear_at":"next_user_message","content":[{"type":"text","text":"old","cache_control":{"type":"ephemeral"}}]},
		{"role":"user","content":"q2"},
		{"role":"system","clear_at":"never","content":[{"type":"text","text":"keep","cache_control":{"type":"ephemeral"}}]}
	]}`
	out, hasClearAt, err := prepareClaudeTurnScopedMessages([]byte(body))
	require.NoError(t, err)
	assert.True(t, hasClearAt)
	assert.False(t, gjson.GetBytes(out, "messages.1.content.0.cache_control").Exists(), "turn-scoped messages may not carry cache_control")
	assert.True(t, gjson.GetBytes(out, "messages.3.content.0.cache_control").Exists(), "clear_at never keeps its breakpoint")
	assert.Equal(t, "next_user_message", gjson.GetBytes(out, "messages.1.clear_at").String())

	plain, hasClearAt, err := prepareClaudeTurnScopedMessages([]byte(`{"messages":[{"role":"user","content":"q"}]}`))
	require.NoError(t, err)
	assert.False(t, hasClearAt)
	assert.JSONEq(t, `{"messages":[{"role":"user","content":"q"}]}`, string(plain))
}

func TestClaudeStableBaseBreakpoint(t *testing.T) {
	cases := []struct {
		name, body, path, ttl string
		added                 bool
	}{
		{"system foundation", `{"system":[{"type":"text","text":"instructions"},{"type":"text","text":"dynamic context"}],"messages":[]}`, "system.0.cache_control", "5m", true},
		{"plain system", `{"system":"instructions"}`, "system.0.cache_control", "5m", true},
		{"tools only", `{"tools":[{"name":"a"},{"name":"b"}]}`, "tools.1.cache_control", "5m", true},
		{"dynamic system protects tools", `{"tools":[{"name":"a"}],"system":[{"type":"text","text":"<total_tokens>9"}]}`, "tools.0.cache_control", "5m", true},
		{"mixed TTL ordering", `{"system":[{"type":"text","text":"instructions"}],"messages":[{"role":"user","content":[{"type":"text","text":"q","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`, "system.0.cache_control", "1h", true},
		{"existing foundation", `{"system":[{"type":"text","text":"instructions","cache_control":{"type":"ephemeral","ttl":"1h"}}]}`, "", "", false},
		{"full slots", `{"tools":[{"name":"a","cache_control":{}},{"name":"b","cache_control":{}},{"name":"c","cache_control":{}}],"system":[{"type":"text","text":"instructions"}],"cache_control":{"type":"ephemeral"}}`, "", "", false},
		{"no foundation", `{"messages":[{"role":"user","content":"hello"}]}`, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, added, err := addClaudeStableBaseBreakpoint([]byte(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.added, added)
			if !tc.added {
				assert.Equal(t, tc.body, string(out))
				return
			}
			assert.Equal(t, tc.ttl, gjson.GetBytes(out, tc.path+".ttl").String())
			again, addedAgain, err := addClaudeStableBaseBreakpoint(out)
			require.NoError(t, err)
			assert.False(t, addedAgain)
			assert.Equal(t, string(out), string(again))
		})
	}
}
