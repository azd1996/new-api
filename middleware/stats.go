package middleware

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

// StatsMiddleware 统计中间件
func StatsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		common.BeginRelayRequest()
		defer common.CompleteRelayRequest()
		c.Next()
	}
}

// StatsInfo 统计信息结构
type StatsInfo struct {
	ActiveConnections int64 `json:"active_connections"`
}

// GetStats 获取统计信息
func GetStats() StatsInfo {
	return StatsInfo{
		ActiveConnections: common.GetHTTPRuntimeStats().ActiveRequests,
	}
}
