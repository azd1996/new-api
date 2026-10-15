package service

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := common.Marshal(v)
	require.NoError(t, err)
	return json.RawMessage(data)
}

func paramOpPaths(t *testing.T, override map[string]interface{}) []string {
	t.Helper()
	ops, ok := override["operations"].([]interface{})
	require.True(t, ok, "operations should be a []interface{}")
	paths := make([]string, 0, len(ops))
	for _, op := range ops {
		m, ok := op.(map[string]interface{})
		require.True(t, ok)
		path, _ := m["path"].(string)
		paths = append(paths, path)
	}
	return paths
}

// setOverrideTemplatesForTest mutates the global setting (via its live pointer)
// and returns a restore func, so resolver tests are deterministic and isolated.
func setOverrideTemplatesForTest(param, retry []operation_setting.OverrideTemplateItem) func() {
	s := operation_setting.GetOverrideTemplateSetting()
	prevParam, prevRetry := s.ParamTemplates, s.RetryTemplates
	s.ParamTemplates, s.RetryTemplates = param, retry
	return func() {
		s.ParamTemplates, s.RetryTemplates = prevParam, prevRetry
	}
}

func TestMergeParamOverrideTemplatesOrder(t *testing.T) {
	op := func(path string) map[string]interface{} {
		return map[string]interface{}{"mode": "delete", "path": path}
	}
	restore := setOverrideTemplatesForTest([]operation_setting.OverrideTemplateItem{
		{Name: "t1", Content: rawJSON(t, map[string]interface{}{"operations": []interface{}{op("a")}})},
		{Name: "t2", Content: rawJSON(t, map[string]interface{}{"operations": []interface{}{op("b")}})},
	}, nil)
	defer restore()

	base := map[string]interface{}{"operations": []interface{}{op("own")}}

	// Templates run first in selection order, the channel's own op stays LAST.
	merged := MergeParamOverrideTemplates([]string{"t1", "t2"}, base)
	assert.Equal(t, []string{"a", "b", "own"}, paramOpPaths(t, merged))

	// Unknown references are skipped, order of the known ones preserved.
	merged = MergeParamOverrideTemplates([]string{"missing", "t2", "t1"}, base)
	assert.Equal(t, []string{"b", "a", "own"}, paramOpPaths(t, merged))

	// No references returns the base untouched.
	assert.Equal(t, []string{"own"}, paramOpPaths(t, MergeParamOverrideTemplates(nil, base)))
}

func TestResolveRetryOverrideTemplatesOrder(t *testing.T) {
	rule := func(desc string) map[string]interface{} {
		return map[string]interface{}{"description": desc}
	}
	restore := setOverrideTemplatesForTest(nil, []operation_setting.OverrideTemplateItem{
		{Name: "r1", Content: rawJSON(t, []interface{}{rule("one")})},
		{Name: "r2", Content: rawJSON(t, []interface{}{rule("two-a"), rule("two-b")})},
		{Name: "bad", Content: rawJSON(t, map[string]interface{}{"not": "an array"})},
	})
	defer restore()

	rules := ResolveRetryOverrideTemplates([]string{"r1", "r2"})
	require.Len(t, rules, 3)
	assert.Equal(t, "one", rules[0].Description)
	assert.Equal(t, "two-a", rules[1].Description)
	assert.Equal(t, "two-b", rules[2].Description)

	// Unknown and unparseable references are skipped silently.
	rules = ResolveRetryOverrideTemplates([]string{"missing", "bad", "r1"})
	require.Len(t, rules, 1)
	assert.Equal(t, "one", rules[0].Description)

	assert.Empty(t, ResolveRetryOverrideTemplates(nil))
}
