package controller

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryOverrideReturnErrorTerminates protects the return_error contract from
// both ports: a matched return_error rule terminates the relay loop WITHOUT
// mutating apiErr (so the error log keeps the original upstream status/message),
// while handing the relay loop a terminate directive carrying the configured
// override that is applied to the response only after logging.
func TestRetryOverrideReturnErrorTerminates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	// Object form of phase4 with a return_error action, custom code and message.
	setting := `{"retry_override":[{"description":"hard stop on policy block","phase2_response_conditions":{"conditions":[{"path":"status_code","mode":"full","value":400}]},"phase4_retry_action":{"action":"return_error","status_code":451,"status_msg":"blocked by policy"}}]}`
	channel := &model.Channel{Id: 21, Setting: &setting}
	info := &relaycommon.RelayInfo{UserId: 1}
	apiErr := types.NewErrorWithStatusCode(errors.New("upstream rejected"), types.ErrorCodeBadResponse, 400)

	triggered, terminate, detail := maybeApplyRetryRuleFallback(c, info, channel, apiErr, types.RelayFormatOpenAI, &service.RetryParam{})
	assert.False(t, triggered, "terminate must not request a retry")
	require.NotNil(t, terminate, "return_error signals terminate")
	assert.Equal(t, 451, terminate.StatusCode)
	assert.Equal(t, "blocked by policy", terminate.Message)
	require.NotNil(t, detail)
	assert.False(t, info.RetryFallbackDone, "terminate neither pins nor consumes the one-shot retry")

	// apiErr is NOT mutated by the hook: the error log must record the original.
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Equal(t, "upstream rejected", apiErr.Error())

	// The audit detail carries the override for the admin-only retry_override log.
	assert.Equal(t, retryrule.ActionReturnError, detail["action"])
	assert.Equal(t, 451, detail["return_status"])
	assert.Equal(t, "blocked by policy", detail["return_message"])

	// Only after logging does the relay loop apply the override to what the caller
	// sees (mirrors controller.Relay's terminate branch).
	apiErr.StatusCode = terminate.StatusCode
	apiErr.SetMessage(terminate.Message)
	assert.Equal(t, 451, apiErr.StatusCode)
	assert.Equal(t, "blocked by policy", apiErr.Error())
}
