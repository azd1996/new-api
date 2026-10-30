package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionRejectsNegativeClaudeDefaultMaxTokens(t *testing.T) {
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/",
		strings.NewReader(`{"key":"claude.default_max_tokens","value":"{\"default\":-1}"}`),
	)

	UpdateOption(context)

	assert.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.False(t, payload.Success)
	assert.Contains(t, payload.Message, "-1")
}

// Malformed override-template libraries are rejected before they are persisted.
func TestUpdateOptionRejectsInvalidOverrideTemplates(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"not an array", `{"name":"a"}`, "Override templates must be a valid JSON array"},
		{"missing name", `[{"content":{}}]`, "Override template #1 is missing a name"},
		{"duplicate name", `[{"name":"a","content":{}},{"name":"a","content":{}}]`, "Duplicate override template name: a"},
		{"missing content", `[{"name":"a"}]`, "Override template a is missing content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := common.Marshal(map[string]string{"key": "override_template_setting.param_templates", "value": tc.value})
			require.NoError(t, err)
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(body)))

			UpdateOption(context)

			var payload struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.False(t, payload.Success)
			assert.Equal(t, tc.want, payload.Message)
		})
	}
}
