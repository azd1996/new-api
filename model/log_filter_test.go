package model

import (
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
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

// TestRetryOverrideLogIsAdminOnly pins the visibility contract of the
// retry-override audit log: admins see it with its admin_info detail, while
// the owning user's log list never returns that log type.
func TestRetryOverrideLogIsAdminOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Log{}, &User{}, &Channel{}))

	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	const userId = 7
	require.NoError(t, db.Create(&Log{UserId: userId, Type: LogTypeConsume, Content: "consume"}).Error)
	RecordRetryOverrideLog(OverrideAuditLogParams{UserId: userId, ChannelId: 3, RequestId: "req-1"},
		"retry override matched", map[string]any{"action": "fallback_next_channel"})

	adminLogs, _, err := GetAllLogs(LogFilter{Type: LogTypeRetryOverride}, 0, 10)
	require.NoError(t, err)
	require.Len(t, adminLogs, 1)
	assert.Equal(t, 3, adminLogs[0].ChannelId)
	assert.Contains(t, adminLogs[0].Other, "fallback_next_channel")

	userLogs, total, err := GetUserLogs(userId, LogFilter{}, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, userLogs, 1)
	assert.Equal(t, LogTypeConsume, userLogs[0].Type)

	userLogs, _, err = GetUserLogs(userId, LogFilter{Type: LogTypeRetryOverride}, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, userLogs)
}

// TestLogFilterMatchesAdminInfoCustomerNameAndTraceTag pins the admin filters
// on customer_name (its dedicated indexed column, mirroring customer_id) and
// downstream_trace_tag (which still lives only in the Other JSON).
func TestLogFilterMatchesAdminInfoCustomerNameAndTraceTag(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	oldType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(oldType) })
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	require.NoError(t, db.Create([]Log{
		{Id: 1, CustomerName: "Acme", NodeName: "node-a", Other: `{"admin_info":{"customer_name":"Acme","downstream_trace_tag":"tag-a"}}`},
		{Id: 2, CustomerName: "Other", NodeName: "node-b", Other: `{"admin_info":{"customer_name":"Other","downstream_trace_tag":"tag-b"}}`},
	}).Error)

	findIDs := func(filter LogFilter) []int {
		query, applyErr := filter.apply(db.Model(&Log{}))
		require.NoError(t, applyErr)
		var ids []int
		require.NoError(t, query.Order("id").Pluck("id", &ids).Error)
		return ids
	}

	assert.Equal(t, []int{1}, findIDs(LogFilter{CustomerName: "Acme"}))
	assert.Equal(t, []int{2}, findIDs(LogFilter{DownstreamTraceTag: "tag-b"}))
	assert.Empty(t, findIDs(LogFilter{CustomerName: "Acme", DownstreamTraceTag: "tag-b"}))
	assert.Equal(t, []int{2}, findIDs(LogFilter{NodeName: LogFieldFilter{Values: []string{"node-b"}}}))
	assert.Equal(t, []int{1}, findIDs(LogFilter{NodeName: LogFieldFilter{Values: []string{"node-b"}, Exclude: true}}))
	assert.Empty(t, findIDs(LogFilter{NodeName: LogFieldFilter{Values: []string{"node-c"}}}))
}

func TestSuccessCountsRetainCustomerAndTraceFilters(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Log{}))
	oldDB, oldType := LOG_DB, common.LogDatabaseType()
	t.Cleanup(func() { LOG_DB = oldDB; common.SetLogDatabaseType(oldType) })
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.Create([]Log{
		{ChannelId: 1, Type: LogTypeConsume, CustomerName: "Acme", Other: `{"admin_info":{"customer_name":"Acme","downstream_trace_tag":"a"}}`},
		{ChannelId: 1, Type: LogTypeError, CustomerName: "Acme", Other: `{"admin_info":{"customer_name":"Acme","downstream_trace_tag":"b"}}`},
		{ChannelId: 2, Type: LogTypeConsume, CustomerName: "Other", Other: `{"admin_info":{"customer_name":"Other","downstream_trace_tag":"b"}}`},
	}).Error)
	for _, tc := range []struct {
		name, customer, trace string
		consume, errors       map[int]int64
	}{
		{"customer", "Acme", "", map[int]int64{1: 1}, map[int]int64{1: 1}},
		{"trace", "", "b", map[int]int64{2: 1}, map[int]int64{1: 1}},
		{"combined", "Acme", "a", map[int]int64{1: 1}, map[int]int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			consume, errs, _, err := countConsumeErrorByChannel(coreFilterForSuccess(LogFilter{
				CustomerName: tc.customer, DownstreamTraceTag: tc.trace,
				Type: LogTypeError, ErrorKeyword: "does-not-match",
			}))
			require.NoError(t, err)
			assert.Equal(t, tc.consume, consume)
			assert.Equal(t, tc.errors, errs)
		})
	}
}

// TestLogSessionsClickHouse pins the session table contract on a real
// ClickHouse: the materialized view folds new consume/error logs with a real
// session id into log_sessions, listing uses lifetime totals filtered by
// lifetime overlap and the kept dimensions, intervals come from consume logs,
// and the backfill loads pre-view history exactly once. Point
// TEST_CLICKHOUSE_DSN at a scratch database; the test drops and recreates logs.
func TestLogSessionsClickHouse(t *testing.T) {
	dsn := os.Getenv("TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("TEST_CLICKHOUSE_DSN is not configured")
	}
	db, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	prevDB, prevType, prevPause := LOG_DB, common.LogDatabaseType(), logSessionsBackfillPause
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	logSessionsBackfillPause = 0
	t.Cleanup(func() {
		LOG_DB = prevDB
		common.SetLogDatabaseType(prevType)
		logSessionsBackfillPause = prevPause
	})
	for _, table := range []string{"log_sessions_mv", "log_sessions", "log_sessions_backfill", "logs"} {
		require.NoError(t, db.Exec("DROP TABLE IF EXISTS "+table).Error)
	}

	// History written before the session table existed.
	require.NoError(t, db.Exec(clickHouseLogCreateTableSQL(0)).Error)
	yesterday := time.Now().AddDate(0, 0, -1).Unix()
	require.NoError(t, db.Create([]Log{
		{Id: 1, UserId: 1, Username: "alice", TokenName: "k1", ModelName: "claude-x", Type: LogTypeConsume, SessionID: "s-old", CreatedAt: yesterday, Quota: 7, PromptTokens: 10},
		{Id: 2, UserId: 1, Username: "alice", TokenName: "k1", ModelName: "claude-x", Type: LogTypeConsume, SessionID: "s-a", CreatedAt: yesterday, Quota: 1, PromptTokens: 100},
	}).Error)

	// Startup migration is idempotent.
	require.NoError(t, migrateClickHouseLogDB())
	require.NoError(t, migrateClickHouseLogDB())

	claudeHit := `{"claude":true,"cache_tokens":3000,"cache_creation_tokens":0}`
	claudeCold := `{"claude":true,"cache_tokens":0,"cache_creation_tokens":2000}`
	// Live rows land after the view was created; created_at tracks insert
	// time in production, so keep them past the backfill cutoff.
	var cutoff int64
	require.NoError(t, db.Raw("SELECT toInt64(toUnixTimestamp(metadata_modification_time)) FROM system.tables WHERE database = currentDatabase() AND name = 'log_sessions_mv'").Scan(&cutoff).Error)
	now := cutoff + 400
	require.NoError(t, db.Create([]Log{
		{Id: 3, UserId: 1, Username: "alice", TokenName: "k1", ModelName: "claude-x", ChannelId: 3, Group: "vip", Type: LogTypeConsume, SessionID: "s-a", CreatedAt: now - 300, Quota: 10, PromptTokens: 100, CompletionTokens: 5, Other: claudeCold},
		{Id: 4, UserId: 1, Username: "alice", TokenName: "k1", ModelName: "claude-x", ChannelId: 3, Group: "vip", Type: LogTypeError, SessionID: "s-a", CreatedAt: now - 299},
		{Id: 5, UserId: 1, Username: "alice", TokenName: "k1", ModelName: "claude-x", ChannelId: 3, Group: "vip", Type: LogTypeConsume, SessionID: "s-a", CreatedAt: now - 270, Quota: 20, PromptTokens: 100, CompletionTokens: 7, Other: claudeHit},
		{Id: 6, UserId: 2, Username: "bob", TokenName: "k2", ModelName: "gpt-x", ChannelId: 4, Group: "default", CustomerName: "Acme", Type: LogTypeConsume, SessionID: "s-b", CreatedAt: now - 100, Quota: 5, PromptTokens: 50},
		{Id: 7, UserId: 2, Username: "bob", Type: LogTypeConsume, SessionID: "notfound", CreatedAt: now},
		{Id: 8, UserId: 2, Username: "bob", Type: LogTypeConsume, SessionID: "", CreatedAt: now},
		{Id: 9, UserId: 2, Username: "bob", Type: LogTypeTopup, SessionID: "s-c", CreatedAt: now},
	}).Error)

	ids := func(sessions []*LogSession) []string {
		out := make([]string, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, s.SessionId)
		}
		return out
	}

	sessions, total, err := GetLogSessions(0, LogFilter{}, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Equal(t, []string{"s-b", "s-a"}, ids(sessions))
	a := sessions[1]
	assert.EqualValues(t, 3, a.RequestCount)
	assert.EqualValues(t, 2, a.ConsumeCount)
	assert.EqualValues(t, 1, a.ErrorCount)
	assert.EqualValues(t, now-300, a.FirstAt)
	assert.EqualValues(t, 30, a.Quota)
	assert.Equal(t, "alice", a.Username)
	assert.Equal(t, "k1", a.TokenName)
	assert.EqualValues(t, 1, a.ModelCount)
	assert.EqualValues(t, 30, a.MinInterval)
	assert.EqualValues(t, 30, a.MaxInterval)
	assert.Equal(t, 1, a.Cache.Hit)
	assert.Equal(t, 1, a.Cache.ColdWrite)
	assert.EqualValues(t, 3000, a.Cache.CacheRead)
	assert.EqualValues(t, 5200, a.Cache.InputAll)

	// Lifetime overlap: a window after s-a's last request lists only s-b;
	// a window ending before s-b started lists only s-a.
	sessions, _, err = GetLogSessions(0, LogFilter{StartTimestamp: now - 200}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"s-b"}, ids(sessions))
	sessions, _, err = GetLogSessions(0, LogFilter{EndTimestamp: now - 280}, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"s-a"}, ids(sessions))

	// Kept dimensions filter sessions; others are ignored.
	for _, tc := range []struct {
		name   string
		filter LogFilter
		want   []string
	}{
		{"model like", LogFilter{ModelName: LogFieldFilter{Values: []string{"claude-%"}}}, []string{"s-a"}},
		{"model exclude", LogFilter{ModelName: LogFieldFilter{Values: []string{"claude-x"}, Exclude: true}}, []string{"s-b"}},
		{"channel", LogFilter{Channel: LogChannelFilter{Values: []int{4}}}, []string{"s-b"}},
		{"group", LogFilter{Group: LogFieldFilter{Values: []string{"vip"}}}, []string{"s-a"}},
		{"customer", LogFilter{CustomerName: "Acme"}, []string{"s-b"}},
		{"session id", LogFilter{SessionId: "s-a"}, []string{"s-a"}},
		{"ignored field", LogFilter{RequestId: "nope", StatusCode: 500}, []string{"s-b", "s-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions, _, err := GetLogSessions(0, tc.filter, 0, 10)
			require.NoError(t, err)
			assert.Equal(t, tc.want, ids(sessions))
		})
	}

	own, total, err := GetLogSessions(2, LogFilter{}, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, []string{"s-b"}, ids(own))

	// Backfill loads the pre-view rows once; a second run skips both days.
	runLogSessionsBackfill(cutoff, 2)
	runLogSessionsBackfill(cutoff, 2)
	status := GetLogSessionsBackfillStatus()
	assert.Empty(t, status.LastError)
	assert.Len(t, status.Skipped, 2)

	sessions, total, err = GetLogSessions(0, LogFilter{}, 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Equal(t, []string{"s-b", "s-a", "s-old"}, ids(sessions))
	assert.EqualValues(t, 4, sessions[1].RequestCount)
	assert.EqualValues(t, 31, sessions[1].Quota)
	assert.EqualValues(t, yesterday, sessions[1].FirstAt)
	assert.EqualValues(t, 7, sessions[2].Quota)
}
