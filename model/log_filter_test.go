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
