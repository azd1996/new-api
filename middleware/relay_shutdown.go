package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// RelayShutdown registers each relay request with common.RelayRequests so a
// graceful-shutdown timeout can force-cancel still-running upstream calls and
// mark their logs. New requests arriving after draining begins get a 503.
func RelayShutdown() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, done, ok := common.RelayRequests.Begin(c.Request.Context())
		if !ok {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{"type": "server_shutdown", "message": "server is shutting down"},
			})
			return
		}
		defer done()
		c.Request = c.Request.WithContext(ctx)
		if c.Request.Body != nil {
			stop := common.TrackRelayCloser(ctx, c.Request.Body)
			defer stop()
		}
		c.Next()
	}
}
