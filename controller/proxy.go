package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// GetProxyHealth returns the passive health snapshot (status + connect-latency
// percentiles) of every alias in the global proxy table, for the admin UI.
func GetProxyHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    service.ProxyHealthSnapshot(),
	})
}
