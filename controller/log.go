package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// logFilterValues reads a log filter field as a multi-value list. A field may
// repeat in the query string and/or carry comma-separated values; blanks are
// dropped. A single value yields a one-element list, matching the former
// scalar query params.
func logFilterValues(c *gin.Context, key string) []string {
	var values []string
	for _, raw := range c.QueryArray(key) {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}

// logFilterExclude reports whether the companion "<key>_op" param selects the
// exclude (not-equals) operator. Any other value, including absence, means
// include (the default).
func logFilterExclude(c *gin.Context, key string) bool {
	return strings.EqualFold(strings.TrimSpace(c.Query(key+"_op")), "exclude")
}

func logFieldFilterFromQuery(c *gin.Context, key string) model.LogFieldFilter {
	return model.LogFieldFilter{Values: logFilterValues(c, key), Exclude: logFilterExclude(c, key)}
}

func logChannelFilterFromQuery(c *gin.Context) model.LogChannelFilter {
	var values []int
	for _, value := range logFilterValues(c, "channel") {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			values = append(values, parsed)
		}
	}
	return model.LogChannelFilter{Values: values, Exclude: logFilterExclude(c, "channel")}
}

// logFilterFromQuery reads the log search shared by the log list and the log
// export. Username and channel are read only for administrators.
func logFilterFromQuery(c *gin.Context, admin bool) model.LogFilter {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	filter := model.LogFilter{
		Type:              logType,
		StartTimestamp:    startTimestamp,
		EndTimestamp:      endTimestamp,
		ModelName:         logFieldFilterFromQuery(c, "model_name"),
		TokenName:         logFieldFilterFromQuery(c, "token_name"),
		Group:             logFieldFilterFromQuery(c, "group"),
		RequestId:         c.Query("request_id"),
		UpstreamRequestId: c.Query("upstream_request_id"),
		SessionId:         c.Query("session_id"),
		ErrorKeyword:      c.Query("error_keyword"),
	}
	if sc, e := strconv.Atoi(c.Query("status_code")); e == nil {
		filter.StatusCode = sc
	}
	if admin {
		filter.Username = logFieldFilterFromQuery(c, "username")
		filter.Channel = logChannelFilterFromQuery(c)
		filter.DownstreamRequestId = c.Query("downstream_request_id")
		filter.CustomerId = c.Query("customer_id")
		filter.ErrorSignatureHash = c.Query("error_signature_hash")
		// retry_triggered is a tri-state admin filter: absent = no filter; only
		// the canonical "true"/"false" select retry / non-retry error logs.
		switch c.Query("retry_triggered") {
		case "true":
			v := true
			filter.RetryTriggered = &v
		case "false":
			v := false
			filter.RetryTriggered = &v
		}
	}
	return filter
}

// limitLogTimeRange returns the start of a log or dashboard search kept within
// the longest log time range. A missing start reaches back the whole range, and
// time after now does not count toward it.
func limitLogTimeRange(startTimestamp int64, endTimestamp int64) (int64, error) {
	maxRange := operation_setting.GetLogSetting().MaxRangeSeconds()
	end := common.GetTimestamp()
	if endTimestamp != 0 {
		end = min(end, endTimestamp)
	}
	if startTimestamp == 0 {
		return end - maxRange, nil
	}
	if end-startTimestamp > maxRange {
		return 0, common.NewMessage("The time range cannot exceed {{days}} days", map[string]any{"days": maxRange / (24 * 60 * 60)})
	}
	return startTimestamp, nil
}

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logs, total, err := model.GetAllLogs(logFilterFromQuery(c, true), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if c.GetInt("role") < common.RoleRootUser {
		model.FormatAdminLogs(logs)
	} else {
		model.FormatRootLogs(logs)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

// GetLogsAnalysis aggregates the admin log list (same filter set as GetAllLogs,
// scoped to consume logs) into the prompt-size latency/FRT tables, the cache-hit
// table, and the per-channel success-rate table shown in the log-analysis
// popup. Admin only.
func GetLogsAnalysis(c *gin.Context) {
	result, err := model.GetLogAnalysis(logFilterFromQuery(c, true))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	filter := logFilterFromQuery(c, false)
	if c.GetInt("role") < common.RoleAdminUser {
		startTimestamp, err := limitLogTimeRange(filter.StartTimestamp, filter.EndTimestamp)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		filter.StartTimestamp = startTimestamp
	}
	logs, total, err := model.GetUserLogs(c.GetInt("id"), filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

// ExportAllLogs streams every user's logs matching the search.
func ExportAllLogs(c *gin.Context) {
	exportLogs(c, true)
}

// ExportUserLogs streams the caller's own logs matching the search.
func ExportUserLogs(c *gin.Context) {
	if c.GetInt("role") < common.RoleAdminUser && !operation_setting.GetLogSetting().UserExportEnabled {
		common.ApiErrorT(c, "Log export is disabled")
		return
	}
	exportLogs(c, false)
}

// exportLogs writes one JSON log per line, newest first, and ends with an API
// response object: success with the log count, or the failure. Each user runs
// one export at a time, and every export stays within the longest log time
// range.
func exportLogs(c *gin.Context, all bool) {
	filter := logFilterFromQuery(c, all)
	startTimestamp, err := limitLogTimeRange(filter.StartTimestamp, filter.EndTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	filter.StartTimestamp = startTimestamp

	userId := c.GetInt("id")
	release, ok, err := service.AcquireLogExportLock(userId)
	if err != nil {
		common.SysError(common.LogText("failed to lock log export: %s", err.Error()))
		common.ApiErrorT(c, "Failed to export logs")
		return
	}
	if !ok {
		common.ApiErrorT(c, "Another log export is running. Try again after it finishes.")
		return
	}
	defer release()

	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	count := 0
	writeLog := func(log *model.Log) error {
		line, err := common.Marshal(log)
		if err != nil {
			return err
		}
		if _, err := c.Writer.Write(append(line, '\n')); err != nil {
			return err
		}
		count++
		return nil
	}
	ctx := c.Request.Context()
	if all {
		err = model.StreamAllLogs(ctx, filter, c.GetInt("role"), writeLog)
	} else {
		err = model.StreamUserLogs(ctx, userId, filter, writeLog)
	}
	if ctx.Err() != nil {
		return
	}
	// The status is already sent, so the result object becomes the last line.
	if err != nil {
		common.SysError(common.LogText("failed to export logs: %s", err.Error()))
		common.ApiErrorT(c, "Failed to export logs")
		return
	}
	common.ApiSuccess(c, gin.H{"count": count})
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	common.ApiErrorT(c, "This API is deprecated")
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	common.ApiErrorT(c, "This API is deprecated")
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		common.ApiErrorI18n(c, i18n.MsgTokenInvalid)
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
	tokenName := logFieldFilterFromQuery(c, "token_name")
	username := logFieldFilterFromQuery(c, "username")
	modelName := logFieldFilterFromQuery(c, "model_name")
	channel := logChannelFilterFromQuery(c)
	group := logFieldFilterFromQuery(c, "group")
	stat, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, "")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": stat.Quota,
			"rpm":   stat.Rpm,
			"tpm":   stat.Tpm,
		},
	})
	return
}

func GetLogsSelfStat(c *gin.Context) {
	username := c.GetString("username")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if c.GetInt("role") < common.RoleAdminUser {
		var err error
		if startTimestamp, err = limitLogTimeRange(startTimestamp, endTimestamp); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	tokenName := logFieldFilterFromQuery(c, "token_name")
	modelName := logFieldFilterFromQuery(c, "model_name")
	channel := logChannelFilterFromQuery(c)
	group := logFieldFilterFromQuery(c, "group")
	usernameFilter := model.LogFieldFilter{Values: []string{username}}
	quotaNum, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, usernameFilter, tokenName, channel, group)
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
