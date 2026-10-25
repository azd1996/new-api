package controller

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/retryrule"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRetryOverrideErrorAttemptAudit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.User{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "retry-audit", Setting: "{}"}).Error)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldEnabled, oldRedis := constant.ErrorLogEnabled, common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	constant.ErrorLogEnabled, common.RedisEnabled = true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		constant.ErrorLogEnabled, common.RedisEnabled = oldEnabled, oldRedis
	})

	for _, action := range []string{retryrule.ActionRetrySameChannel, retryrule.ActionFallbackNextChannel} {
		t.Run(action, func(t *testing.T) {
			require.NoError(t, db.Exec("DELETE FROM logs").Error)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			c.Set("id", 1)
			c.Set("channel_id", 17)
			setting := `{"retry_override":[{"description":"thinking failure","phase2_response_conditions":{"conditions":[{"path":"status_code","mode":"full","value":400}]},"phase3_request_rewrite":[{"mode":"delete","path":"thinking"}],"phase4_retry_action":"` + action + `"}]}`
			channel := &model.Channel{Id: 17, Setting: &setting}
			info := &relaycommon.RelayInfo{UserId: 1}
			apiErr := types.NewErrorWithStatusCode(errors.New("thinking unsupported"), types.ErrorCodeBadResponse, 400)
			triggered, detail := maybeApplyRetryRuleFallback(c, info, channel, apiErr, types.RelayFormatOpenAI, &service.RetryParam{})
			require.True(t, triggered)
			require.NotNil(t, detail)
			processChannelError(c, types.ChannelError{ChannelId: 17}, apiErr, types.RelayFormatOpenAI, detail)

			// A later failed attempt on the same request has no matching override.
			if action == retryrule.ActionFallbackNextChannel {
				apiErr = types.NewErrorWithStatusCode(errors.New("unavailable"), types.ErrorCodeBadResponse, 503)
			}
			triggered, detail = maybeApplyRetryRuleFallback(c, info, channel, apiErr, types.RelayFormatOpenAI, &service.RetryParam{})
			assert.False(t, triggered)
			assert.Nil(t, detail)
			processChannelError(c, types.ChannelError{ChannelId: 17}, apiErr, types.RelayFormatOpenAI, detail)
			// Ordinary retry/error writers do not receive override metadata.
			processChannelError(c, types.ChannelError{ChannelId: 17}, apiErr, types.RelayFormatOpenAI)

			var logs []model.Log
			require.NoError(t, db.Order("id").Find(&logs).Error)
			require.Len(t, logs, 4)
			assert.Equal(t, model.LogTypeRetryOverride, logs[0].Type)
			assert.True(t, logs[1].RetryTriggered)
			var audit, first map[string]interface{}
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &audit))
			require.NoError(t, common.UnmarshalJsonStr(logs[1].Other, &first))
			assert.Equal(t, true, first["retry_triggered"])
			auditDetail := audit["admin_info"].(map[string]interface{})["retry_override"]
			errorDetail := first["admin_info"].(map[string]interface{})["retry_override"]
			assert.Equal(t, auditDetail, errorDetail)
			assert.Equal(t, []interface{}{float64(0)}, errorDetail.(map[string]interface{})["rule_ids"])
			assert.Equal(t, []interface{}{"thinking failure"}, errorDetail.(map[string]interface{})["rule_descriptions"])
			for _, log := range logs[2:] {
				assert.False(t, log.RetryTriggered)
				var other map[string]interface{}
				require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
				assert.Equal(t, false, other["retry_triggered"])
				assert.NotContains(t, other["admin_info"], "retry_override")
			}
		})
	}
}

func TestParseAdminRetryTriggered(t *testing.T) {
	for _, value := range []string{"", "true", "false", "yes", "no", "1", "TRUE"} {
		t.Run(value, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/api/log/?retry_triggered="+value, nil)
			filters := parseLogFilters(c)
			err := parseAdminRetryTriggered(c, &filters)
			switch value {
			case "":
				require.NoError(t, err)
				assert.Nil(t, filters.RetryTriggered)
			case "true", "false":
				require.NoError(t, err)
				require.NotNil(t, filters.RetryTriggered)
				assert.Equal(t, value == "true", *filters.RetryTriggered)
			default:
				require.EqualError(t, err, "retry_triggered must be true or false")
			}
		})
	}
}
