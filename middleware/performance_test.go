package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestSystemPerformanceCheckLogsRejectionBeforeAuth verifies the gateway-entry
// overload rejection records an error log with admin-only system diagnostics,
// attributes nothing to a user (userId == 0 must not query user settings), and
// dual-writes the admin error_signature_hash into the real column.
func TestSystemPerformanceCheckLogsRejectionBeforeAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	// No user table is migrated on purpose: an unauthenticated rejection must not
	// query user settings.
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldEnabled, oldNode := constant.ErrorLogEnabled, common.NodeName
	oldConfig := common.GetPerformanceMonitorConfig()
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldProvider := systemStatusProvider
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		constant.ErrorLogEnabled, common.NodeName = oldEnabled, oldNode
		common.SetPerformanceMonitorConfig(oldConfig)
		common.SetDatabaseTypes(oldMainType, oldLogType)
		systemStatusProvider = oldProvider
	})
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.NodeName = "overload-test-node"
	config := common.PerformanceMonitorConfig{Enabled: true, OverloadConsecutiveSamples: 1, CPUThreshold: 90, MemoryThreshold: 90, DiskThreshold: 95}

	for _, tc := range []struct {
		name          string
		path          string
		status        common.SystemStatus
		protectionOff bool
		loggingOff    bool
		code          string
	}{
		{name: "cpu_openai", path: "/v1/chat/completions", status: common.SystemStatus{CPUUsage: 95, CPUOverloaded: true}, code: "system_cpu_overloaded"},
		{name: "memory_claude", path: "/v1/messages", status: common.SystemStatus{MemoryUsage: 97, MemoryOverloaded: true}, code: "system_memory_overloaded"},
		{name: "logging_disabled", path: "/v1/chat/completions", status: common.SystemStatus{CPUUsage: 99, CPUOverloaded: true}, loggingOff: true, code: "system_cpu_overloaded"},
		{name: "protection_disabled", path: "/v1/chat/completions", status: common.SystemStatus{CPUUsage: 99, CPUOverloaded: true}, protectionOff: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Where("1 = 1").Delete(&model.Log{}).Error)
			cfg := config
			if tc.protectionOff {
				cfg.OverloadConsecutiveSamples = 0
			}
			common.SetPerformanceMonitorConfig(cfg)
			constant.ErrorLogEnabled = !tc.loggingOff
			status := tc.status
			status.SampledAt = time.Now()
			systemStatusProvider = func() common.SystemStatus { return status }

			body := strings.NewReader(`{"model":"must-not-be-parsed"}`)
			bodyLen := body.Len()
			nextCalled := false
			r := gin.New()
			r.Use(RequestId(), SystemPerformanceCheck())
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

			if tc.loggingOff {
				assert.Empty(t, logs)
				return
			}

			require.Len(t, logs, 1)
			log := logs[0]
			assert.Equal(t, model.LogTypeError, log.Type)
			assert.Zero(t, log.UserId)
			assert.Zero(t, log.TokenId)
			assert.Zero(t, log.ChannelId)
			assert.Empty(t, log.Username)
			assert.Empty(t, log.ModelName)
			assert.Empty(t, log.Ip)
			assert.NotEmpty(t, log.RequestId)
			assert.Equal(t, w.Header().Get(common.RequestIdKey), log.RequestId)
			assert.NotEmpty(t, log.ErrorSignatureHash)
			assert.NotContains(t, log.Other, "secret")

			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, tc.path, other["request_path"])
			assert.Equal(t, tc.code, other["error_code"])
			assert.Equal(t, float64(503), other["status_code"])

			admin, ok := other["admin_info"].(map[string]any)
			require.True(t, ok, "admin diagnostics must be nested under admin_info")
			assert.Equal(t, common.NodeName, admin["node_name"])
			// error_signature_hash is admin-tier and dual-written to the column.
			assert.Equal(t, log.ErrorSignatureHash, admin["error_signature_hash"])
			detail, ok := admin["system_overload"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, false, detail["authenticated"])
			assert.Equal(t, float64(cfg.CPUThreshold), detail["cpu_threshold"])
			assert.Equal(t, float64(cfg.MemoryThreshold), detail["memory_threshold"])
			assert.Equal(t, float64(cfg.DiskThreshold), detail["disk_threshold"])
			assert.Equal(t, tc.status.CPUUsage, detail["cpu_usage"])
			assert.Equal(t, tc.status.MemoryUsage, detail["memory_usage"])
			assert.Equal(t, tc.status.DiskUsage, detail["disk_usage"])
		})
	}
}
