package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSystemPerformanceCheckLogsRejectionBeforeAuth(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldConfig, oldEnabled, oldNode := common.GetPerformanceMonitorConfig(), constant.ErrorLogEnabled, common.NodeName
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetPerformanceMonitorConfig(oldConfig)
		constant.ErrorLogEnabled, common.NodeName = oldEnabled, oldNode
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})
	// No user table: an unauthenticated rejection must not query user settings.
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.NodeName = "overload-test-node"
	config := common.PerformanceMonitorConfig{
		Enabled:                    true,
		OverloadConsecutiveSamples: 1,
		CPUThreshold:               90,
		MemoryThreshold:            90,
		DiskThreshold:              95,
	}
	var cpuSignature string
	for _, tc := range []struct {
		name         string
		path         string
		status       common.SystemStatus
		monitorOff   bool
		loggingOff   bool
		thresholdOff bool
		code         string
	}{
		{name: "cpu", path: "/v1/chat/completions", status: common.SystemStatus{CPUUsage: 91.2, MemoryUsage: 99, DiskUsage: 99, CPUOverloaded: true}, code: "system_cpu_overloaded"},
		{name: "cpu_changed_usage", path: "/v1/chat/completions", status: common.SystemStatus{CPUUsage: 98.7, CPUOverloaded: true}, code: "system_cpu_overloaded"},
		{name: "memory_claude", path: "/v1/messages", status: common.SystemStatus{MemoryUsage: 92.3, MemoryOverloaded: true}, code: "system_memory_overloaded"},
		{name: "disk", path: "/v1beta/models/test:generateContent", status: common.SystemStatus{DiskUsage: 96.4, DiskOverloaded: true}, code: "system_disk_overloaded"},
		{name: "logging_disabled", path: "/v1/messages", status: common.SystemStatus{CPUUsage: 99, CPUOverloaded: true}, loggingOff: true, code: "system_cpu_overloaded"},
		{name: "monitor_disabled", path: "/v1/chat/completions", status: common.SystemStatus{CPUUsage: 99}, monitorOff: true},
		{name: "integer_threshold_boundary", path: "/v1/chat/completions", status: common.SystemStatus{CPUUsage: 90.9, MemoryUsage: 90.9, DiskUsage: 95.9}},
		{name: "threshold_disabled", path: "/v1/chat/completions", status: common.SystemStatus{CPUUsage: 99}, thresholdOff: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Where("1 = 1").Delete(&model.Log{}).Error)
			cfg := config
			cfg.Enabled = !tc.monitorOff
			if tc.monitorOff {
				cfg.OverloadConsecutiveSamples = 0
			}
			if tc.thresholdOff {
				cfg.CPUThreshold = 0
			}
			common.SetPerformanceMonitorConfig(cfg)
			constant.ErrorLogEnabled = !tc.loggingOff
			body := strings.NewReader(`{"model":"must-not-be-parsed"}`)
			bodyLen := body.Len()
			nextCalled := false
			r := gin.New()
			r.Use(RequestId(), systemPerformanceCheck(func() common.SystemStatus { return tc.status }))
			r.POST(tc.path, func(c *gin.Context) {
				nextCalled = true
				c.Status(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, tc.path+"?secret=not-logged", body))
			assert.Equal(t, bodyLen, body.Len(), "performance protection must not read the request body")
			var logs []model.Log
			require.NoError(t, db.Find(&logs).Error)
			if tc.code == "" {
				assert.Equal(t, http.StatusNoContent, w.Code)
				assert.True(t, nextCalled)
				assert.Empty(t, logs)
				return
			}
			assert.False(t, nextCalled, "authentication/distribution/charging must not run")
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
			expected := checkSystemPerformance(cfg, tc.status)
			var responseError interface{} = expected.ToOpenAIError()
			if strings.HasPrefix(tc.path, "/v1/messages") {
				responseError = expected.ToClaudeError()
			}
			expectedJSON, err := common.Marshal(gin.H{"error": responseError})
			require.NoError(t, err)
			assert.JSONEq(t, string(expectedJSON), w.Body.String())
			if tc.loggingOff {
				assert.Empty(t, logs)
				return
			}
			require.Len(t, logs, 1)
			log := logs[0]
			assert.Equal(t, model.LogTypeError, log.Type)
			assert.NotEmpty(t, w.Header().Get(common.RequestIdKey))
			assert.Equal(t, w.Header().Get(common.RequestIdKey), log.RequestId)
			assert.Zero(t, log.UserId)
			assert.Zero(t, log.TokenId)
			assert.Zero(t, log.ChannelId)
			assert.Empty(t, log.Username)
			assert.Empty(t, log.ModelName)
			assert.Empty(t, log.TokenName)
			assert.Empty(t, log.Group)
			assert.Empty(t, log.UpstreamRequestId)
			assert.Empty(t, log.Ip)
			assert.Zero(t, log.Quota)
			assert.Zero(t, log.PromptTokens)
			assert.Zero(t, log.CompletionTokens)
			assert.Equal(t, expected.ErrorWithStatusCode(), log.Content)
			assert.NotContains(t, log.Other, "secret")
			var other map[string]interface{}
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, tc.path, other["request_path"])
			assert.Equal(t, tc.code, other["error_code"])
			assert.Equal(t, float64(503), other["status_code"])
			assert.NotEmpty(t, log.ErrorSignatureHash)
			assert.Equal(t, log.ErrorSignatureHash, other["error_signature_hash"])
			adminLogs, total, err := model.GetAllLogs(model.LogTypeError, 0, 0, model.LogFilterOptions{},
				0, 10, log.RequestId, "", 503, "", "", "", "", "", log.ErrorSignatureHash)
			require.NoError(t, err)
			assert.EqualValues(t, 1, total)
			require.Len(t, adminLogs, 1)
			assert.Equal(t, log.Other, adminLogs[0].Other)
			userLogs, total, err := model.GetUserLogs(123, model.LogTypeError, 0, 0, model.LogFilterOptions{},
				0, 10, log.RequestId, "", 0, "", "", "", "")
			require.NoError(t, err)
			assert.Zero(t, total)
			assert.Empty(t, userLogs, "unauthenticated rejections must not be attributed to a user")
			admin := other["admin_info"].(map[string]interface{})
			assert.Equal(t, common.NodeName, admin["node_name"])
			detail := admin["system_overload"].(map[string]interface{})
			assert.Equal(t, tc.status.CPUUsage, detail["cpu_usage"])
			assert.Equal(t, tc.status.MemoryUsage, detail["memory_usage"])
			assert.Equal(t, tc.status.DiskUsage, detail["disk_usage"])
			assert.Equal(t, float64(cfg.CPUThreshold), detail["cpu_threshold"])
			assert.Equal(t, float64(cfg.MemoryThreshold), detail["memory_threshold"])
			assert.Equal(t, float64(cfg.DiskThreshold), detail["disk_threshold"])
			assert.Equal(t, false, detail["authenticated"])
			if tc.name == "cpu" {
				cpuSignature = log.ErrorSignatureHash
			} else if tc.name == "cpu_changed_usage" {
				assert.Equal(t, cpuSignature, log.ErrorSignatureHash, "usage changes must not split error groups")
			}
		})
	}
}
