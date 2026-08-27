package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const maxCslHourlySpanSeconds = int64(7 * 24 * 3600)

// cslHourlyLastSecondOffset turns an hour-aligned timestamp into the last second
// of that same hour (mm:59:59), so an `end` hour is queried inclusively.
const cslHourlyLastSecondOffset = int64(3599)

// cslHourlyHourLayouts are the accepted layouts for the hour-granularity `start`
// and `end` parameters, e.g. "2026-08-27-17" or "2026-0827-17".
var cslHourlyHourLayouts = []string{"2006-01-02-15", "2006-0102-15"}

const cslHourlyHourFormatHint = `must be an hour like "2026-08-27-17" (YYYY-MM-DD-HH in server local time)`

// parseCslHourlyTimeRange resolves the query time range. The hour-granularity
// `start`/`end` parameters take precedence over `start_timestamp`/`end_timestamp`;
// `end` defaults to `start` when omitted.
// Returns (start, end, ok); writes an error response and returns ok=false on failure.
func parseCslHourlyTimeRange(c *gin.Context) (int64, int64, bool) {
	start, end := strings.TrimSpace(c.Query("start")), strings.TrimSpace(c.Query("end"))
	if start != "" || end != "" {
		return parseCslHourlyHourRange(c, start, end)
	}
	return parseCslHourlyTimestampRange(c)
}

// parseCslHourlyHourRange converts an hour-granularity range into Unix seconds
// spanning the whole `start` hour through the whole `end` hour, then applies the
// same 7-day span limit as the timestamp form.
func parseCslHourlyHourRange(c *gin.Context, start, end string) (int64, int64, bool) {
	startTimestamp, ok := parseCslHourlyHour(start)
	if !ok {
		common.ApiErrorMsg(c, "invalid start, it "+cslHourlyHourFormatHint)
		return 0, 0, false
	}
	endTimestamp := startTimestamp
	if end != "" {
		endTimestamp, ok = parseCslHourlyHour(end)
		if !ok {
			common.ApiErrorMsg(c, "invalid end, it "+cslHourlyHourFormatHint)
			return 0, 0, false
		}
	}
	if endTimestamp < startTimestamp {
		common.ApiErrorMsg(c, "invalid time range")
		return 0, 0, false
	}
	if endTimestamp-startTimestamp > maxCslHourlySpanSeconds {
		common.ApiErrorMsg(c, "time range must not exceed 7 days")
		return 0, 0, false
	}
	return startTimestamp, endTimestamp + cslHourlyLastSecondOffset, true
}

// parseCslHourlyHour parses an hour string such as "2026-08-27-17" into the Unix
// second of that hour's first second (mm:00:00) in server local time.
func parseCslHourlyHour(value string) (int64, bool) {
	for _, layout := range cslHourlyHourLayouts {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return parsed.Unix(), true
		}
	}
	return 0, false
}

// parseCslHourlyTimestampRange parses and validates start_timestamp and
// end_timestamp query parameters. Both are required, must be positive, and the
// span must not exceed 7 days. Each value is floored to the nearest hour boundary.
func parseCslHourlyTimestampRange(c *gin.Context) (int64, int64, bool) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid start_timestamp")
		return 0, 0, false
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid end_timestamp")
		return 0, 0, false
	}
	if endTimestamp < startTimestamp {
		common.ApiErrorMsg(c, "invalid time range")
		return 0, 0, false
	}
	startTimestamp = startTimestamp - startTimestamp%3600
	endTimestamp = endTimestamp - endTimestamp%3600
	if endTimestamp-startTimestamp > maxCslHourlySpanSeconds {
		common.ApiErrorMsg(c, "time range must not exceed 7 days")
		return 0, 0, false
	}
	return startTimestamp, endTimestamp, true
}

func handleCslHourly(c *gin.Context, isAdmin bool) {
	if isAdmin && c.GetInt("role") < common.RoleAdminUser {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "insufficient privilege",
		})
		return
	}
	startTimestamp, endTimestamp, ok := parseCslHourlyTimeRange(c)
	if !ok {
		return
	}
	params := model.CslHourlyQuery{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ModelName:      c.Query("model_name"),
	}
	if isAdmin {
		params.GroupName = c.GetString("group_name")
	} else {
		params.Username = c.GetString("username")
		params.TokenName = c.Query("token_name")
	}
	rows, err := model.GetCslHourly(params)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"message":     "",
		"data":        rows,
		"query_start": startTimestamp,
		"query_end":   endTimestamp,
	})
}

// GetAllCslHourly returns csl_hourly rows for every user within the caller's
// group. Requires role >= RoleAdminUser.
func GetAllCslHourly(c *gin.Context) {
	handleCslHourly(c, true)
}

// GetUserCslHourly returns csl_hourly rows for the authenticated user only.
func GetUserCslHourly(c *gin.Context) {
	handleCslHourly(c, false)
}
