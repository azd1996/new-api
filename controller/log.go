package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	statusCode, _ := strconv.Atoi(c.Query("status_code"))
	errorKeyword := c.Query("error_keyword")
	customerId := c.Query("customer_id")
	customerName := c.Query("customer_name")
	downstreamRequestId := c.Query("downstream_request_id")
	downstreamTraceTag := c.Query("downstream_trace_tag")
	errorSignatureHash := c.Query("error_signature_hash")
	logs, total, err := model.GetAllLogs(logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId, upstreamRequestId, statusCode, errorKeyword, customerId, customerName, downstreamRequestId, downstreamTraceTag, errorSignatureHash)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	statusCode, _ := strconv.Atoi(c.Query("status_code"))
	customerId := c.Query("customer_id")
	customerName := c.Query("customer_name")
	downstreamRequestId := c.Query("downstream_request_id")
	downstreamTraceTag := c.Query("downstream_trace_tag")
	logs, total, err := model.GetUserLogs(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId, upstreamRequestId, statusCode, customerId, customerName, downstreamRequestId, downstreamTraceTag)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

// parseSessionLogSearchParams reads the shared session-search query params
// (session_id / username / time range) used by both start and poll endpoints.
func parseSessionLogSearchParams(c *gin.Context) service.SessionLogSearchParams {
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	return service.SessionLogSearchParams{
		SessionID: c.Query("session_id"),
		Username:  c.Query("username"),
		StartSec:  start,
		EndSec:    end,
	}
}

// StartSessionLogSearch kicks off (or serves from cache) an async session-id
// log search. Admin only. force=true backs up any cached result and rescans.
func StartSessionLogSearch(c *gin.Context) {
	p := parseSessionLogSearchParams(c)
	p.Force = c.Query("force") == "true"
	st, err := service.StartSessionLogSearch(p)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, st)
}

// GetSessionLogSearch returns the current status plus a page of matched rows
// for a prior StartSessionLogSearch. Admin only.
func GetSessionLogSearch(c *gin.Context) {
	p := parseSessionLogSearchParams(c)
	if err := service.ValidateSessionLogSearch(p); err != nil {
		common.ApiError(c, err)
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 0 {
		page = 0
	}
	size, _ := strconv.Atoi(c.Query("page_size"))
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	st, ok := service.QuerySessionLogSearch(p)
	items, total := service.ReadSessionLogSearch(p, page, size)
	common.ApiSuccess(c, gin.H{
		"found":     ok,
		"status":    st,
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": size,
	})
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无效的令牌",
		})
		return
	}
	logs, err := model.GetLogByTokenId(tokenId)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	stat, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// Reporting-only amounts (channel-discounted / customer-discounted / profit)
	// live per row in the Other JSON, so they are aggregated separately and honor
	// the full admin log-list filter set (customer, status code, trace-tag, ...).
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	statusCode, _ := strconv.Atoi(c.Query("status_code"))
	errorKeyword := c.Query("error_keyword")
	customerId := c.Query("customer_id")
	customerName := c.Query("customer_name")
	downstreamRequestId := c.Query("downstream_request_id")
	downstreamTraceTag := c.Query("downstream_trace_tag")
	errorSignatureHash := c.Query("error_signature_hash")
	amounts, err := model.SumLogStatAmounts(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, requestId, upstreamRequestId, statusCode, errorKeyword, customerId, customerName, downstreamRequestId, downstreamTraceTag, errorSignatureHash)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota":               stat.Quota,
			"rpm":                 stat.Rpm,
			"tpm":                 stat.Tpm,
			"channel_stat_quota":  amounts.ChannelStatQuota,
			"customer_stat_quota": amounts.CustomerStatQuota,
			"profit_quota":        amounts.ProfitQuota,
		},
	})
	return
}

func GetLogsSelfStat(c *gin.Context) {
	username := c.GetString("username")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	quotaNum, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, tokenName)
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": quotaNum.Quota,
			"rpm":   quotaNum.Rpm,
			"tpm":   quotaNum.Tpm,
			//"token": tokenNum,
		},
	})
	return
}

// DeleteHistoryLogs is the legacy synchronous log cleanup endpoint (DELETE /api/log/).
// It deletes directly instead of going through the async system task. It is kept only
// for the classic frontend; the default frontend uses POST /api/system-task/log-cleanup.
// TODO: remove this handler (and its route) once the classic frontend is removed.
func DeleteHistoryLogs(c *gin.Context) {
	targetTimestamp, _ := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if targetTimestamp == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}
	count, err := model.DeleteOldLog(c.Request.Context(), targetTimestamp, 100)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
	return
}
