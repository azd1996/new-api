package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const maxCslHourlySpanSeconds = int64(7 * 24 * 3600)

// parseCslHourlyTimeRange parses and validates start_timestamp and end_timestamp
// query parameters. Both are required, must be positive, and the span must not
// exceed 7 days. Each value is floored to the nearest hour boundary.
// Returns (start, end, ok); writes an error response and returns ok=false on failure.
func parseCslHourlyTimeRange(c *gin.Context) (int64, int64, bool) {
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
