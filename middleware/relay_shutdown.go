package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

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
