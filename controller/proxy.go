package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type proxyChannelRef struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Status int    `json:"status"`
}

// GetProxyChannels returns, per proxy alias, the channels whose
// setting.proxy_aliases reference it, so the proxy pool UI can show usage.
func GetProxyChannels(c *gin.Context) {
	var channels []*model.Channel
	if err := model.DB.Select("id", "name", "status", "setting").Order("id asc").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	refs := make(map[string][]proxyChannelRef)
	for _, channel := range channels {
		for _, alias := range channel.GetSetting().ProxyAliases {
			refs[alias] = append(refs[alias], proxyChannelRef{Id: channel.Id, Name: channel.Name, Status: channel.Status})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    refs,
	})
}

// GetProxyHealth returns the passive health snapshot (status + connect-latency
// percentiles) of every alias in the global proxy table, for the admin UI.
func GetProxyHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    service.ProxyHealthSnapshot(),
	})
}
