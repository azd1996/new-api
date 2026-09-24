package middleware

import (
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/pkg/prom_metrics"

	"github.com/gin-gonic/gin"
)

// PrometheusMiddleware records per-request HTTP metrics. It uses the matched
// route template (c.FullPath) rather than the raw URL to keep label cardinality
// bounded. It is only attached to the engine when metrics export is enabled.
func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		prommetrics.ObserveHTTP(
			c.Request.Method,
			path,
			strconv.Itoa(c.Writer.Status()),
			time.Since(start).Seconds(),
		)
	}
}
