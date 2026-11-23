package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTestCustomerCacheOptimization covers the admin dry-run endpoint: the
// request contract binds, the production pipeline runs per item, and the
// response reports applied rules, skipped state-dependent rules and headers.
func TestTestCustomerCacheOptimization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	itemBody := `{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`
	req := customerCacheOptimizationTestRequest{
		Config: &model.CustomerClaudeCacheOptimization{
			ForceExtendedTTL:         model.CustomerClaudeCacheOptimizationRule{Enabled: true},
			RetainPreviousBreakpoint: model.CustomerClaudeCacheOptimizationRule{Enabled: true},
			FallbackCacheBreakpoint:  model.CustomerClaudeCacheOptimizationRule{Enabled: true},
		},
		Group: "default",
		Items: []customerCacheOptimizationTestItem{{Body: itemBody}},
	}
	payload, err := common.Marshal(req)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/customer/cache_optimization/test", strings.NewReader(string(payload)))
	c.Request.Header.Set("Content-Type", "application/json")
	TestCustomerCacheOptimization(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := w.Body.String()
	assert.Contains(t, body, `"success":true`)
	assert.Contains(t, body, `"ok":true`)
	assert.Contains(t, body, "cache_control")
	assert.Contains(t, body, `"enabled_rules":["force_extended_ttl","retain_previous_breakpoint","fallback_cache_breakpoint"]`)
	assert.Contains(t, body, `"skipped_steps":["retain_previous_breakpoint"]`)
	assert.Contains(t, body, `"headers":{"anthropic-beta":"extended-cache-ttl-2025-04-11"}`)
}

func TestTestCustomerCacheOptimizationRejectsUnknownMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload, err := common.Marshal(customerCacheOptimizationTestRequest{
		Mode:  "bogus",
		Items: []customerCacheOptimizationTestItem{{Body: `{"model":"claude-sonnet-4","messages":[]}`}},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/customer/cache_optimization/test", strings.NewReader(string(payload)))
	c.Request.Header.Set("Content-Type", "application/json")
	TestCustomerCacheOptimization(c)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"success":false`)
	assert.Contains(t, w.Body.String(), "cache_optimization")
}
