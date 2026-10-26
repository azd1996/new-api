package model

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupLogShutdownFixture(t *testing.T) (*gorm.DB, User) {
	t.Helper()

	// Match the model TestMain fixture, but isolate rows and restore every
	// global changed here. These tests must not run in parallel.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&User{}, &Log{}))

	originalDB, originalLogDB := DB, LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalDataExportEnabled := common.DataExportEnabled
	t.Cleanup(func() {
		DB, LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		common.RedisEnabled = originalRedisEnabled
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.DataExportEnabled = originalDataExportEnabled
	})
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.LogConsumeEnabled = true
	common.DataExportEnabled = false

	settings, err := common.Marshal(dto.UserSetting{RecordIpLog: true})
	require.NoError(t, err)
	user := User{
		Username:  "shutdown-log-user",
		Setting:   string(settings),
		Quota:     10000,
		UsedQuota: 123,
	}
	require.NoError(t, db.Create(&user).Error)
	return db, user
}

func TestRecordLogsProcessShutdown(t *testing.T) {
	shutdown := common.ProcessShutdownInfo{
		Instance: "gateway-test-instance",
		BatchID:  "shutdown-test-batch",
		At:       1792886400,
		Reason:   "shutdown_timeout",
	}
	for _, tc := range []struct {
		name          string
		drain         bool
		force         bool
		existingOther bool
	}{
		{name: "normal", existingOther: true},
		{name: "drain_without_force", drain: true, existingOther: true},
		{name: "forced_preserves_admin_info", force: true, existingOther: true},
		{name: "forced_allocates_other", force: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, user := setupLogShutdownFixture(t)
			registry := common.NewRelayRequestRegistry()
			ctx, done, ok := registry.Begin(context.Background())
			require.True(t, ok)
			t.Cleanup(done)
			if tc.drain {
				registry.Drain()
			}
			if tc.force {
				require.Equal(t, 1, registry.ForceCancel(shutdown))
			}

			// Use the tracked HTTP request context, as relay handlers do. Do
			// not call AttachProcessShutdownInfo from the test: both recording
			// APIs must attach the marker themselves before serialization.
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
			c.Request.RemoteAddr = "192.0.2.10:12345"
			c.Set("username", user.Username)
			c.Set(common.RequestIdKey, "shutdown-log-request")
			c.Set(common.UpstreamRequestIdKey, "shutdown-log-upstream")

			for _, logType := range []int{LogTypeError, LogTypeConsume} {
				var other map[string]interface{}
				if tc.existingOther {
					other = map[string]interface{}{
						"model_price": 0.004,
						"admin_info": map[string]interface{}{
							"channel_debug": "keep-existing-detail",
						},
					}
				}
				if logType == LogTypeError {
					RecordErrorLog(c, user.Id, 17, "shutdown-model", "shutdown-token",
						"upstream interrupted", 29, 3, true, "default", other)
				} else {
					RecordConsumeLog(c, user.Id, RecordConsumeLogParams{
						ChannelId:        17,
						PromptTokens:     11,
						CompletionTokens: 7,
						ModelName:        "shutdown-model",
						TokenName:        "shutdown-token",
						Quota:            42,
						Content:          "partial stream usage",
						TokenId:          29,
						UseTimeSeconds:   3,
						IsStream:         true,
						Group:            "default",
						Other:            other,
					})
				}
			}
			done()
			waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, registry.Wait(waitCtx))

			var logs []Log
			require.NoError(t, db.Order("id").Find(&logs).Error)
			require.Len(t, logs, 2, "both recording APIs must actually persist a SQLite row")
			for i, log := range logs {
				assert.Positive(t, log.Id)
				assert.Equal(t, []int{LogTypeError, LogTypeConsume}[i], log.Type)
				assert.Equal(t, user.Id, log.UserId)
				assert.Equal(t, user.Username, log.Username)
				assert.Equal(t, "shutdown-log-request", log.RequestId)
				assert.Equal(t, "shutdown-log-upstream", log.UpstreamRequestId)
				assert.Equal(t, "192.0.2.10", log.Ip, "the explicit user setting must be honored")
				if log.Type == LogTypeConsume {
					assert.Equal(t, 42, log.Quota)
					assert.Equal(t, 11, log.PromptTokens)
					assert.Equal(t, 7, log.CompletionTokens)
				} else {
					assert.Zero(t, log.Quota)
					assert.Zero(t, log.PromptTokens)
					assert.Zero(t, log.CompletionTokens)
				}

				other, err := common.StrToMap(log.Other)
				require.NoError(t, err)
				assert.NotContains(t, other, "process_shutdown", "shutdown metadata must remain admin-only")
				if tc.force || tc.existingOther {
					adminInfo, ok := other["admin_info"].(map[string]interface{})
					require.True(t, ok, "persisted admin_info must be a JSON object")
					if tc.existingOther {
						assert.Equal(t, "keep-existing-detail", adminInfo["channel_debug"])
						assert.Equal(t, 0.004, other["model_price"])
					}
					if tc.force {
						assert.Equal(t, map[string]interface{}{
							"instance": shutdown.Instance,
							"batch_id": shutdown.BatchID,
							"at":       float64(shutdown.At),
							"reason":   shutdown.Reason,
						}, adminInfo["process_shutdown"])
					} else {
						assert.NotContains(t, adminInfo, "process_shutdown")
					}
				} else {
					assert.NotContains(t, other, "admin_info")
				}
			}

			// Exercise a real user-facing DB query as well as the raw rows.
			userLogs, err := GetLogByTokenId(29)
			require.NoError(t, err)
			require.Len(t, userLogs, 2)
			for _, log := range userLogs {
				other, err := common.StrToMap(log.Other)
				require.NoError(t, err)
				assert.NotContains(t, other, "admin_info")
				assert.NotContains(t, other, "process_shutdown")
				if tc.existingOther {
					assert.Equal(t, 0.004, other["model_price"])
				}
			}

			var persistedUser User
			require.NoError(t, db.First(&persistedUser, user.Id).Error)
			assert.Equal(t, user.Quota, persistedUser.Quota, "log recording must not change the user's balance")
			assert.Equal(t, user.UsedQuota, persistedUser.UsedQuota)
		})
	}
}
