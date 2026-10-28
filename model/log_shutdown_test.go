package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestRecordErrorLogMarksProcessShutdown proves a request force-cancelled by a
// process shutdown gets an admin-tier process_shutdown marker on its error log,
// without any user/channel attribution (userId 0 must not query user settings).
func TestRecordErrorLogMarksProcessShutdown(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Log{}))

	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	registry := common.NewRelayRequestRegistry()
	ctx, done, ok := registry.Begin(context.Background())
	require.True(t, ok)
	defer done()
	info := common.ProcessShutdownInfo{Instance: "node-x", BatchID: "batch-1", At: 1792886400, Reason: "shutdown_timeout"}
	registry.ForceCancel(info)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	other := NewLogOther()
	RecordErrorLog(c, 0, 0, "", "", "interrupted", 0, 0, false, "", other)

	var logs []Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, LogTypeError, logs[0].Type)
	assert.Zero(t, logs[0].UserId)

	var stored map[string]any
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &stored))
	admin, ok := stored["admin_info"].(map[string]any)
	require.True(t, ok, "process_shutdown marker must live in the admin tier")
	shutdown, ok := admin["process_shutdown"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "batch-1", shutdown["batch_id"])
	assert.Equal(t, "shutdown_timeout", shutdown["reason"])
	assert.Equal(t, "node-x", shutdown["instance"])
}
