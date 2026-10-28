package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRelayShutdownRejectsNewRequestsWhileDraining verifies the relay-shutdown
// middleware admits requests normally and, once draining begins, rejects new
// ones with a 503 server_shutdown instead of starting fresh upstream work.
func TestRelayShutdownRejectsNewRequestsWhileDraining(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := common.RelayRequests
	common.RelayRequests = common.NewRelayRequestRegistry()
	t.Cleanup(func() { common.RelayRequests = original })

	r := gin.New()
	r.Use(RelayShutdown())
	r.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusNoContent, w1.Code)

	common.RelayRequests.Drain()
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusServiceUnavailable, w2.Code)
	assert.Contains(t, w2.Body.String(), "server_shutdown")
}
