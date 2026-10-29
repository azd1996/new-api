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

// TestLogFilterApplyIncludeExclude asserts the multi-value include/exclude
// contract of LogFilter.apply over the five admin filter fields, including
// single-value parity (a lone include value equals the former scalar filter).
func TestLogFilterApplyIncludeExclude(t *testing.T) {
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

	findIDs := func(filter LogFilter) []int {
		query, applyErr := filter.apply(db.Session(&gorm.Session{}))
		require.NoError(t, applyErr)
		var rows []logFilterTestRow
		require.NoError(t, query.Order("id").Find(&rows).Error)
		ids := make([]int, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		return ids
	}

	// Mixed exclude + multi-value include across every field narrows to nothing.
	assert.Empty(t, findIDs(LogFilter{
		ModelName: LogFieldFilter{Values: []string{"gpt-%"}, Exclude: true},
		TokenName: LogFieldFilter{Values: []string{"prod", "test"}},
		Channel:   LogChannelFilter{Values: []int{1, 3}, Exclude: true},
		Group:     LogFieldFilter{Values: []string{"default"}},
	}))

	// Multi-value include (LIKE), exclude list, and channel IN keep only row 2.
	assert.Equal(t, []int{2}, findIDs(LogFilter{
		ModelName: LogFieldFilter{Values: []string{"gpt-%", "claude-%"}},
		Username:  LogFieldFilter{Values: []string{"alice", "carol"}, Exclude: true},
		Channel:   LogChannelFilter{Values: []int{1, 2}},
		Group:     LogFieldFilter{Values: []string{"default", "vip"}},
	}))

	// Single include value behaves exactly like the former scalar filters.
	assert.Equal(t, []int{1}, findIDs(LogFilter{
		TokenName: LogFieldFilter{Values: []string{"prod"}},
		Channel:   LogChannelFilter{Values: []int{1}},
	}))
	assert.Equal(t, []int{1, 3}, findIDs(LogFilter{
		Group: LogFieldFilter{Values: []string{"default"}},
	}))

	// A NOT IN channel filter is the exclude counterpart of the include list.
	assert.Equal(t, []int{2}, findIDs(LogFilter{
		Channel: LogChannelFilter{Values: []int{1, 3}, Exclude: true},
	}))
}

// TestLogLikeConditionDialectCompatibility pins the LIKE escaping the filters
// rely on for ClickHouse versus the escape-clause dialects.
func TestLogLikeConditionDialectCompatibility(t *testing.T) {
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
