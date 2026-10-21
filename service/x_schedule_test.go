package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractSchedulePlaceholders(t *testing.T) {
	got := ExtractSchedulePlaceholders(`topk(__TOPK__, rate(x{model=~"__MODEL__"}[__TIMERANGE__])) > __THRESHOLD__ + __TOPK__`)
	// Distinct, in first-seen order; duplicates collapsed.
	assert.Equal(t, []string{"TOPK", "MODEL", "TIMERANGE", "THRESHOLD"}, got)
}

func TestResolveSchedulePromQL(t *testing.T) {
	promql := `x{model=~"__MODEL__"}[__TIMERANGE__] > __THRESHOLD__`

	resolved, missing := resolveSchedulePromQL(promql, map[string]string{
		"MODEL":     "claude-opus",
		"TIMERANGE": "12h",
		"THRESHOLD": "0.3",
	})
	assert.Empty(t, missing)
	assert.Equal(t, `x{model=~"claude-opus"}[12h] > 0.3`, resolved)

	_, missing = resolveSchedulePromQL(promql, map[string]string{"MODEL": "claude-opus"})
	assert.ElementsMatch(t, []string{"TIMERANGE", "THRESHOLD"}, missing)
}

func TestPreviewSchedulePromQLReducesRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		// Placeholders must already be resolved before hitting the datasource.
		assert.Equal(t, `sum(x{model=~"m"})`, r.FormValue("query"))
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "readonly", user)
		assert.Equal(t, "secret", pass)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[
			{"metric":{"api_key_name":"cust-a","model":"m"},"value":[1700000000,"0.85"]},
			{"metric":{"api_key_name":"cust-b","model":"m"},"value":[1700000000,"0.42"]}
		]}}`))
	}))
	defer server.Close()

	ds := &model.XScheduleDatasource{BaseURL: server.URL, Username: "readonly", Password: "secret"}
	result, err := PreviewSchedulePromQL(ds, `sum(x{model=~"__MODEL__"})`, "api_key_name", map[string]string{"MODEL": "m"})
	require.NoError(t, err)
	require.Len(t, result.Rows, 2)
	assert.Equal(t, `sum(x{model=~"m"})`, result.ResolvedPromQL)
	assert.Equal(t, "cust-a", result.Rows[0].Label)
	assert.InDelta(t, 0.85, result.Rows[0].Value, 1e-9)
	assert.Equal(t, "cust-b", result.Rows[1].Label)
	assert.InDelta(t, 0.42, result.Rows[1].Value, 1e-9)
}

func TestPreviewSchedulePromQLRejectsUnresolvedPlaceholders(t *testing.T) {
	ds := &model.XScheduleDatasource{BaseURL: "http://127.0.0.1:0"}
	_, err := PreviewSchedulePromQL(ds, `x{model=~"__MODEL__"}`, "model", map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MODEL")
}
