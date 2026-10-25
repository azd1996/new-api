package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type logFilterTestRow struct {
	ID        int    `gorm:"column:id;primaryKey"`
	ModelName string `gorm:"column:model_name"`
	Username  string `gorm:"column:username"`
	TokenName string `gorm:"column:token_name"`
	ChannelID int    `gorm:"column:channel_id"`
	Group     string `gorm:"column:group"`
}

func (logFilterTestRow) TableName() string {
	return "logs"
}

func TestApplyLogFilterOptions(t *testing.T) {
	originalGroupColumn := logGroupCol
	logGroupCol = "`group`"
	t.Cleanup(func() {
		logGroupCol = originalGroupColumn
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&logFilterTestRow{}))
	require.NoError(t, db.Create([]logFilterTestRow{
		{ID: 1, ModelName: "gpt-4", Username: "alice", TokenName: "prod", ChannelID: 1, Group: "default"},
		{ID: 2, ModelName: "claude-3", Username: "bob", TokenName: "prod", ChannelID: 2, Group: "vip"},
		{ID: 3, ModelName: "gpt-4o", Username: "carol", TokenName: "test", ChannelID: 3, Group: "default"},
	}).Error)

	filters := LogFilterOptions{
		ModelName: LogFieldFilter{Values: []string{"gpt-%"}, Exclude: true},
		TokenName: LogFieldFilter{Values: []string{"prod", "test"}},
		Channel:   LogChannelFilter{Values: []int{1, 3}, Exclude: true},
		Group:     LogFieldFilter{Values: []string{"default"}},
	}
	query, err := applyLogFilterOptions(db, filters, "")
	require.NoError(t, err)

	var rows []logFilterTestRow
	require.NoError(t, query.Order("id").Find(&rows).Error)
	require.Len(t, rows, 0)

	filters = LogFilterOptions{
		ModelName: LogFieldFilter{Values: []string{"gpt-%", "claude-%"}},
		Username:  LogFieldFilter{Values: []string{"alice", "carol"}, Exclude: true},
		Channel:   LogChannelFilter{Values: []int{1, 2}},
		Group:     LogFieldFilter{Values: []string{"default", "vip"}},
	}
	query, err = applyLogFilterOptions(db, filters, "")
	require.NoError(t, err)
	rows = nil
	require.NoError(t, query.Order("id").Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, 2, rows[0].ID)
}

func TestLogLikeConditionClickHouseCompatibility(t *testing.T) {
	original := common.LogDatabaseType()
	t.Cleanup(func() {
		common.SetLogDatabaseType(original)
	})

	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	condition, pattern, err := buildLogLikeCondition("model_name", `gpt\_%`)
	require.NoError(t, err)
	assert.Equal(t, "model_name LIKE ?", condition)
	assert.Equal(t, `gpt\\\_%`, pattern)

	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	condition, pattern, err = buildLogLikeCondition("model_name", `gpt\_%`)
	require.NoError(t, err)
	assert.Equal(t, "model_name LIKE ? ESCAPE '!'", condition)
	assert.Equal(t, `gpt\!_%`, pattern)
}

func TestRetryTriggeredMigrationAndAdminFilter(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	// A populated table from before the retry marker must migrate without
	// dropping rows; historical failures have no recorded override match.
	require.NoError(t, db.Exec("CREATE TABLE logs (id integer PRIMARY KEY, type integer, content text, other text)").Error)
	require.NoError(t, db.Exec("INSERT INTO logs (id, type, content, other) VALUES (1, 5, 'legacy', '{}')").Error)
	require.NoError(t, db.AutoMigrate(&Log{}))
	require.NoError(t, db.AutoMigrate(&Log{}))
	assert.True(t, db.Migrator().HasIndex(&Log{}, "idx_logs_retry_triggered"))
	var legacy Log
	require.NoError(t, db.First(&legacy, 1).Error)
	assert.Equal(t, "legacy", legacy.Content)
	assert.False(t, legacy.RetryTriggered)
	require.NoError(t, db.Create([]Log{
		{Id: 2, Type: LogTypeError, RetryTriggered: true, RequestId: "matched"},
		{Id: 3, Type: LogTypeError, RequestId: "ordinary"},
		{Id: 4, Type: LogTypeRetryOverride},
		{Id: 5, Type: LogTypeConsume},
	}).Error)
	oldDB, oldGroup := LOG_DB, logGroupCol
	oldType := common.LogDatabaseType()
	LOG_DB, logGroupCol = db, "`group`"
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB, logGroupCol = oldDB, oldGroup
		common.SetLogDatabaseType(oldType)
	})
	yes, no := true, false
	for _, tc := range []struct {
		name    string
		value   *bool
		logType int
		wantIDs []int
	}{
		{"all", nil, 0, []int{5, 4, 3, 2, 1}},
		{"matched", &yes, 0, []int{2}},
		{"unmatched includes legacy", &no, 0, []int{3, 1}},
		{"explicit error type", &yes, LogTypeError, []int{2}},
		{"other type cannot match", &no, LogTypeRetryOverride, []int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs, total, err := GetAllLogs(tc.logType, 0, 0, LogFilterOptions{RetryTriggered: tc.value}, 0, 10, "", "", 0, "", "", "", "", "", "")
			require.NoError(t, err)
			assert.Equal(t, int64(len(tc.wantIDs)), total)
			ids := make([]int, 0, len(logs))
			for _, log := range logs {
				ids = append(ids, log.Id)
			}
			assert.Equal(t, tc.wantIDs, ids)
		})
	}
}
