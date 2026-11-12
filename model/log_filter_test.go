package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
// on customer_name (matched against its dedicated indexed column),
// downstream_trace_tag, and session_id_source (both of which still live only
// in the Other JSON, the latter with multi-value include/exclude semantics).
func TestLogFilterMatchesAdminInfoCustomerNameAndTraceTag(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	oldType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(oldType) })
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	require.NoError(t, db.Create([]Log{
		{Id: 1, CustomerName: "Acme", NodeName: "node-a", Other: `{"session_id_source":"prompt_cache_key","admin_info":{"customer_name":"Acme","downstream_trace_tag":"tag-a"}}`},
		{Id: 2, CustomerName: "Other", NodeName: "node-b", Other: `{"session_id_source":"metadata.user_id","admin_info":{"customer_name":"Other","downstream_trace_tag":"tag-b"}}`},
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
	assert.Equal(t, []int{1}, findIDs(LogFilter{SessionIdSource: LogFieldFilter{Values: []string{"prompt_cache_key"}}}))
	assert.Equal(t, []int{1, 2}, findIDs(LogFilter{SessionIdSource: LogFieldFilter{Values: []string{"prompt_cache_key", "metadata.user_id"}}}))
	assert.Equal(t, []int{2}, findIDs(LogFilter{SessionIdSource: LogFieldFilter{Values: []string{"prompt_cache_key"}, Exclude: true}}))
	assert.Empty(t, findIDs(LogFilter{SessionIdSource: LogFieldFilter{Values: []string{"content-fallback"}}}))
}

// TestLogFilterCustomerFallsBackToOtherJSON pins that the customer_id /
// customer_name filters also match rows whose decoded customer lives only in
// the Other JSON blob (rows written before the dedicated columns were added),
// and that the __none__ sentinel selects rows with no decoded customer.
func TestLogFilterCustomerFallsBackToOtherJSON(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	oldType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(oldType) })
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	require.NoError(t, db.Create([]Log{
		// New row: value in both the indexed column and Other.
		{Id: 1, CustomerId: "cust-new", CustomerName: "NewCo", Other: `{"admin_info":{"customer_id":"cust-new","customer_name":"NewCo"}}`},
		// Legacy row: value only in Other, columns empty (pre-column rows).
		{Id: 2, Other: `{"admin_info":{"customer_id":"cust-legacy","customer_name":"LegacyCo"}}`},
		// No decoded customer at all.
		{Id: 3, Other: `{"admin_info":{}}`},
	}).Error)

	findIDs := func(filter LogFilter) []int {
		query, applyErr := filter.apply(db.Model(&Log{}))
		require.NoError(t, applyErr)
		var ids []int
		require.NoError(t, query.Order("id").Pluck("id", &ids).Error)
		return ids
	}

	assert.Equal(t, []int{1}, findIDs(LogFilter{CustomerId: "cust-new"}))
	assert.Equal(t, []int{1}, findIDs(LogFilter{CustomerName: "NewCo"}))
	// Legacy rows (column empty) are found via the Other JSON fallback.
	assert.Equal(t, []int{2}, findIDs(LogFilter{CustomerId: "cust-legacy"}))
	assert.Equal(t, []int{2}, findIDs(LogFilter{CustomerName: "LegacyCo"}))
	// __none__ selects rows with no decoded customer.
	assert.Equal(t, []int{3}, findIDs(LogFilter{CustomerId: customerNoneFilter}))
}

// TestLogFilterStatusCodeFallsBackToOtherJSON pins the status_code filter to
// match the dedicated indexed column for rows written since the column was
// added, while still finding older rows that only carry it in the Other JSON
// (see RecordErrorLog's StatusCode dual-write).
func TestLogFilterStatusCodeFallsBackToOtherJSON(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	oldType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(oldType) })
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	require.NoError(t, db.Create([]Log{
		// New row: value in both the indexed column and Other.
		{Id: 1, StatusCode: 429, Other: `{"status_code":429}`},
		// Legacy row: value only in Other, column empty (pre-column rows).
		{Id: 2, Other: `{"status_code":429}`},
		// Unrelated status code.
		{Id: 3, StatusCode: 500, Other: `{"status_code":500}`},
	}).Error)

	findIDs := func(filter LogFilter) []int {
		query, applyErr := filter.apply(db.Model(&Log{}))
		require.NoError(t, applyErr)
		var ids []int
		require.NoError(t, query.Order("id").Pluck("id", &ids).Error)
		return ids
	}

	assert.Equal(t, []int{1, 2}, findIDs(LogFilter{StatusCode: 429}))
	assert.Equal(t, []int{3}, findIDs(LogFilter{StatusCode: 500}))
}

// TestLogFilterErrorKeywordAppliesRegardlessOfType pins ErrorKeyword to apply
// whenever it is set, independent of Type. The frontend's Error Keyword input
// never forces the Type selector to "Error" (see
// web/src/features/usage-logs/components/common-logs-filter-bar.tsx), so a
// Type == LogTypeError guard here silently dropped the filter any time an
// admin searched by keyword while Type was left at "All" or set to something
// else — the "通用日志" keyword search bug this test locks against a regression of.
func TestLogFilterErrorKeywordAppliesRegardlessOfType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	oldType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(oldType) })
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	require.NoError(t, db.Create([]Log{
		{Id: 1, Type: LogTypeError, Content: "boom: rate limit exceeded"},
		{Id: 2, Type: LogTypeConsume, Content: "unrelated content"},
		{Id: 3, Type: LogTypeError, Content: "another failure"},
	}).Error)

	findIDs := func(filter LogFilter) []int {
		query, applyErr := filter.apply(db.Model(&Log{}))
		require.NoError(t, applyErr)
		var ids []int
		require.NoError(t, query.Order("id").Pluck("id", &ids).Error)
		return ids
	}

	// Type left unset ("All Types" in the UI): the keyword must still narrow
	// the result instead of being silently ignored.
	assert.Equal(t, []int{1}, findIDs(LogFilter{ErrorKeyword: "boom"}))
	// Explicit Type=Error keeps working alongside the keyword.
	assert.Equal(t, []int{1}, findIDs(LogFilter{Type: LogTypeError, ErrorKeyword: "boom"}))
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

	claudeHit := `{"claude":true,"cache_tokens":3000,"cache_creation_tokens":0,"session_id_source":"prompt_cache_key"}`
	claudeCold := `{"claude":true,"cache_tokens":0,"cache_creation_tokens":2000,"session_id_source":"prompt_cache_key"}`
	// Live rows land after the view was created; created_at tracks insert
	// time in production, so keep them past the backfill cutoff.
	var cutoff int64
	require.NoError(t, db.Raw("SELECT toInt64(toUnixTimestamp(metadata_modification_time)) FROM system.tables WHERE database = currentDatabase() AND name = 'log_sessions_mv'").Scan(&cutoff).Error)
	now := cutoff + 400
	require.NoError(t, db.Create([]Log{
		{Id: 3, UserId: 1, Username: "alice", TokenName: "k1", ModelName: "claude-x", ChannelId: 3, Group: "vip", Type: LogTypeConsume, SessionID: "s-a", CreatedAt: now - 300, Quota: 10, PromptTokens: 100, CompletionTokens: 5, Other: claudeCold},
		{Id: 4, UserId: 1, Username: "alice", TokenName: "k1", ModelName: "claude-x", ChannelId: 3, Group: "vip", Type: LogTypeError, SessionID: "s-a", CreatedAt: now - 299},
		{Id: 5, UserId: 1, Username: "alice", TokenName: "k1", ModelName: "claude-x", ChannelId: 3, Group: "vip", Type: LogTypeConsume, SessionID: "s-a", CreatedAt: now - 270, Quota: 20, PromptTokens: 100, CompletionTokens: 7, Other: claudeHit},
		{Id: 6, UserId: 2, Username: "bob", TokenName: "k2", ModelName: "gpt-x", ChannelId: 4, Group: "default", CustomerName: "Acme", Type: LogTypeConsume, SessionID: "s-b", CreatedAt: now - 100, Quota: 5, PromptTokens: 50, Other: `{"session_id_source":"metadata.user_id"}`},
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

	sessions, total, totalTokens, _, err := GetLogSessions(0, LogFilter{}, 0, 10, LogSessionSortOptions{})
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
	// s-a: (100+5) + (0+0, error log) + (100+7) prompt+completion across its three logs.
	assert.EqualValues(t, 212, a.TotalTokens)
	assert.EqualValues(t, 50, sessions[0].TotalTokens) // s-b: 50 prompt, no completion
	assert.EqualValues(t, 262, totalTokens)
	assert.EqualValues(t, 262, sessions[0].RangeTotalTokens+sessions[1].RangeTotalTokens)

	sessions, _, _, rangeTotalTokens, err := GetLogSessions(0, LogFilter{
		StartTimestamp: now - 350,
		EndTimestamp:   now - 90,
	}, 0, 10, NewLogSessionSortOptions("range_total_tokens", "desc"))
	require.NoError(t, err)
	assert.Equal(t, []string{"s-a", "s-b"}, ids(sessions))
	assert.EqualValues(t, 262, rangeTotalTokens)
	assert.EqualValues(t, 212, sessions[0].RangeTotalTokens)
	assert.EqualValues(t, 50, sessions[1].RangeTotalTokens)

	// Lifetime overlap: a window after s-a's last request lists only s-b;
	// a window ending before s-b started lists only s-a.
	sessions, _, _, _, err = GetLogSessions(0, LogFilter{StartTimestamp: now - 200}, 0, 10, LogSessionSortOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"s-b"}, ids(sessions))
	sessions, _, _, _, err = GetLogSessions(0, LogFilter{EndTimestamp: now - 280}, 0, 10, LogSessionSortOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"s-a"}, ids(sessions))

	// Sorting by total tokens overrides the default newest-activity order.
	sessions, _, _, _, err = GetLogSessions(0, LogFilter{}, 0, 10, NewLogSessionSortOptions("total_tokens", "desc"))
	require.NoError(t, err)
	assert.Equal(t, []string{"s-a", "s-b"}, ids(sessions))
	sessions, _, _, _, err = GetLogSessions(0, LogFilter{}, 0, 10, NewLogSessionSortOptions("total_tokens", "asc"))
	require.NoError(t, err)
	assert.Equal(t, []string{"s-b", "s-a"}, ids(sessions))
	// An unrecognized sort_by falls back to the default order.
	sessions, _, _, _, err = GetLogSessions(0, LogFilter{}, 0, 10, NewLogSessionSortOptions("quota", "desc"))
	require.NoError(t, err)
	assert.Equal(t, []string{"s-b", "s-a"}, ids(sessions))

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
		{"session source", LogFilter{SessionIdSource: LogFieldFilter{Values: []string{"prompt_cache_key"}}}, []string{"s-a"}},
		{"session source exclude", LogFilter{SessionIdSource: LogFieldFilter{Values: []string{"prompt_cache_key"}, Exclude: true}}, []string{"s-b"}},
		{"ignored field", LogFilter{RequestId: "nope", StatusCode: 500}, []string{"s-b", "s-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions, _, _, _, err := GetLogSessions(0, tc.filter, 0, 10, LogSessionSortOptions{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, ids(sessions))
		})
	}

	own, total, _, _, err := GetLogSessions(2, LogFilter{}, 0, 10, LogSessionSortOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, []string{"s-b"}, ids(own))

	// Backfill loads the pre-view rows once; a second run skips both days.
	runLogSessionsBackfill(cutoff, 2)
	runLogSessionsBackfill(cutoff, 2)
	status := GetLogSessionsBackfillStatus()
	assert.Empty(t, status.LastError)
	assert.Len(t, status.Skipped, 2)

	sessions, total, totalTokens, rangeTotalTokens, err = GetLogSessions(0, LogFilter{}, 0, 10, LogSessionSortOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Equal(t, []string{"s-b", "s-a", "s-old"}, ids(sessions))
	assert.EqualValues(t, 4, sessions[1].RequestCount)
	assert.EqualValues(t, 31, sessions[1].Quota)
	assert.EqualValues(t, yesterday, sessions[1].FirstAt)
	assert.EqualValues(t, 7, sessions[2].Quota)
	// s-a backfilled with the yesterday row (100 prompt, 0 completion): 212+100.
	assert.EqualValues(t, 312, sessions[1].TotalTokens)
	assert.EqualValues(t, 10, sessions[2].TotalTokens) // s-old: 10 prompt, 0 completion
	assert.EqualValues(t, 312+50+10, totalTokens)
	assert.EqualValues(t, 312+50+10, rangeTotalTokens)
}

type requestKeyRenameLogRow struct {
	ID    int    `gorm:"column:id;primaryKey"`
	Type  int    `gorm:"column:type"`
	Other string `gorm:"column:other;type:text"`
}

func (requestKeyRenameLogRow) TableName() string {
	return "logs"
}

// TestLogFilterRequestKeyRenamedDatabaseMatrix pins the request body field
// rename filter on every supported log database: it keeps consume and error
// logs whose Other carries the rename audit and nothing else, including a log
// that only mentions the key name inside a string value.
func TestLogFilterRequestKeyRenamedDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousType := common.LogDatabaseType()
			common.SetLogDatabaseType(common.DatabaseType(dialect))
			t.Cleanup(func() {
				common.SetLogDatabaseType(previousType)
				assert.NoError(t, sqlDB.Close())
			})
			versionSQL := "SELECT version()"
			if dialect == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Logf("database version: %s", version)

			switch dialect {
			case "postgres":
				db = db.Begin()
				require.NoError(t, db.Error)
				t.Cleanup(func() { assert.NoError(t, db.Rollback().Error) })
				schemaName := fmt.Sprintf("log_filter_test_%d", time.Now().UnixNano())
				require.NoError(t, db.Exec("CREATE SCHEMA ?", clause.Table{Name: schemaName}).Error)
				require.NoError(t, db.Exec("SET LOCAL search_path TO ?", clause.Table{Name: schemaName}).Error)
			case "mysql":
				require.False(t, db.Migrator().HasTable("logs"), "TEST_MYSQL_DSN must point at a scratch database")
				require.False(t, db.Migrator().HasTable("channels"), "TEST_MYSQL_DSN must point at a scratch database")
				t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&Log{}, &Channel{})) })
			}
			require.NoError(t, db.AutoMigrate(&Log{}, &Channel{}))
			require.NoError(t, db.Create([]requestKeyRenameLogRow{
				{ID: 1, Type: LogTypeConsume, Other: `{"request_key_rename":["rename file_data -> fileData"]}`},
				{ID: 2, Type: LogTypeError, Other: `{"status_code":400,"request_key_rename":["rename parts.[0-1].file_data -> fileData"]}`},
				{ID: 3, Type: LogTypeConsume, Other: `{"po":["set request_key_rename = 1"]}`},
				{ID: 4, Type: LogTypeConsume, Other: `{"requestXkeyXrename":1}`},
				{ID: 5, Type: LogTypeRetryOverride, Other: `{"request_key_rename":["rename a -> b"]}`},
				{ID: 6, Type: LogTypeConsume, Other: ``},
			}).Error)

			query, err := LogFilter{RequestKeyRenamed: true}.apply(db.Model(&requestKeyRenameLogRow{}))
			require.NoError(t, err)
			var ids []int
			require.NoError(t, query.Order("logs.id").Pluck("logs.id", &ids).Error)
			assert.Equal(t, []int{1, 2}, ids)

			oldDB, oldLogDB := DB, LOG_DB
			oldMemory := common.MemoryCacheEnabled
			t.Cleanup(func() {
				DB, LOG_DB = oldDB, oldLogDB
				common.MemoryCacheEnabled = oldMemory
			})
			DB, LOG_DB = db, db
			common.MemoryCacheEnabled = false
			require.NoError(t, db.Create(&Channel{Id: 1, Name: "renamed"}).Error)
			now := time.Now().Unix()
			const renamed = `{"request_key_rename":["rename a -> b"]}`
			logs := []Log{
				{Type: LogTypeConsume, Username: "alice", CreatedAt: now - 3600, Quota: 10, PromptTokens: 20, CompletionTokens: 10, Other: renamed},
				{Type: LogTypeConsume, Username: "alice", CreatedAt: now - 10, Quota: 20, PromptTokens: 40, CompletionTokens: 30, Other: renamed},
				{Type: LogTypeConsume, Username: "bob", CreatedAt: now - 10, Quota: 30, PromptTokens: 60, CompletionTokens: 50, Other: renamed},
				{Type: LogTypeError, Username: "alice", CreatedAt: now - 10, RetryTriggered: true, Quota: 999, PromptTokens: 999, Other: `{"status_code":429,"request_key_rename":["rename a -> b"]}`},
				{Type: LogTypeError, Username: "alice", CreatedAt: now - 10, Quota: 999, PromptTokens: 999, Other: `{"status_code":500,"request_key_rename":["rename a -> b"]}`},
				{Type: LogTypeConsume, Username: "alice", CreatedAt: now - 3600, Quota: 100, Other: `{}`},
				{Type: LogTypeConsume, Username: "alice", CreatedAt: now - 10, Quota: 200, PromptTokens: 300, Other: `{"requestXkeyXrename":1}`},
				{Type: LogTypeError, Username: "alice", CreatedAt: now - 10, RetryTriggered: true, Other: `{"status_code":429}`},
				{Type: LogTypeError, Username: "alice", CreatedAt: now - 10, Other: `{"status_code":500}`},
				{Type: LogTypeRetryOverride, Username: "alice", CreatedAt: now - 10, Quota: 999, PromptTokens: 999, Other: renamed},
			}
			for i := range logs {
				logs[i].TokenName = "aggregate-regression"
				logs[i].ChannelId = 1
			}
			require.NoError(t, db.Create(&logs).Error)
			token := LogFieldFilter{Values: []string{"aggregate-regression"}}
			for _, tc := range []struct {
				name     string
				username LogFieldFilter
				consume  int64
				rpm, tpm int
			}{
				{"admin", LogFieldFilter{}, 3, 2, 180},
				{"self", LogFieldFilter{Values: []string{"alice"}}, 2, 1, 70},
			} {
				t.Run(tc.name, func(t *testing.T) {
					analysis, err := GetLogAnalysis(LogFilter{
						RequestKeyRenamed: true, TokenName: token, Username: tc.username,
						StartTimestamp: now - 4000, EndTimestamp: now + 60,
					})
					require.NoError(t, err)
					assert.EqualValues(t, tc.consume, analysis.Scanned)
					wantSuccess := LogSuccessCell{Total: tc.consume + 1, Success: tc.consume, Error: 2, RetryOverride: 1, Rate: float64(tc.consume) / float64(tc.consume+1)}
					assert.Equal(t, wantSuccess, analysis.Success.Total)
					assert.Equal(t, []LogSuccessCell{wantSuccess}, analysis.Success.Channels)

					// Error drill filters still leave the success denominator intact.
					drilled, err := GetLogAnalysis(LogFilter{
						RequestKeyRenamed: true, TokenName: token, Username: tc.username,
						Type: LogTypeError, StatusCode: 500, ErrorKeyword: "no-match",
						StartTimestamp: now - 4000, EndTimestamp: now + 60,
					})
					require.NoError(t, err)
					assert.Equal(t, wantSuccess, drilled.Success.Total)

					// The requested type is historically ignored, and the rate window
					// stays independent of the historical quota interval.
					stat, err := SumUsedQuota(LogTypeError, now-3700, now-3500, LogFieldFilter{}, tc.username, token, LogChannelFilter{}, LogFieldFilter{}, true)
					require.NoError(t, err)
					assert.Equal(t, Stat{Quota: 10, Rpm: tc.rpm, Tpm: tc.tpm}, stat)
					all, err := SumUsedQuota(LogTypeError, now-3700, now-3500, LogFieldFilter{}, tc.username, token, LogChannelFilter{}, LogFieldFilter{})
					require.NoError(t, err)
					assert.Equal(t, Stat{Quota: 110, Rpm: tc.rpm + 1, Tpm: tc.tpm + 300}, all)
				})
			}
		})
	}
}

// TestLogFilterRetryRuleMatchesRuleDescription pins the retry-rule filter on
// logs written by RecordRetryOverrideLog: substring match on the matched rule
// description only, JSON-escaped characters, LIKE metacharacters as literals,
// commas kept inside one keyword, and exclude semantics.
func TestLogFilterRetryRuleMatchesRuleDescription(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Log{}, &User{}))

	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis := common.RedisEnabled
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
		common.RedisEnabled = oldRedis
	})
	DB, LOG_DB = db, db
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	record := func(requestId string, detail map[string]any) {
		RecordRetryOverrideLog(OverrideAuditLogParams{UserId: 7, ChannelId: 3, RequestId: requestId}, "retry override matched", detail)
	}
	record("decrypt", map[string]any{"action": "retry_same_channel", "rule_descriptions": []string{"case1 解密失败（有function_call时才删id）"}})
	record("quoted", map[string]any{"action": "fallback_next_channel", "rule_descriptions": []string{`case2 "quoted" <b>&`}})
	record("comma", map[string]any{"action": "return_error", "rule_descriptions": []string{"limit a,b"}})
	record("no-description", map[string]any{"action": "retry_same_channel"})
	// The same text outside rule_descriptions must not match.
	record("elsewhere", map[string]any{"action": "retry_same_channel", "error_message": "case1 failed", "rule_descriptions": []string{"unrelated"}})

	findRequestIds := func(filter LogFilter) []string {
		query, applyErr := filter.apply(db.Model(&Log{}))
		require.NoError(t, applyErr)
		var ids []string
		require.NoError(t, query.Order("id").Pluck("request_id", &ids).Error)
		return ids
	}
	include := func(keyword string) LogFilter {
		return LogFilter{RetryRule: LogFieldFilter{Values: []string{keyword}}}
	}

	assert.Equal(t, []string{"decrypt"}, findRequestIds(include("case1")))
	assert.Equal(t, []string{"decrypt"}, findRequestIds(include("解密失败")))
	assert.Equal(t, []string{"decrypt"}, findRequestIds(include("function_call")), "underscore is literal")
	assert.Empty(t, findRequestIds(include("function.call")))
	assert.Equal(t, []string{"quoted"}, findRequestIds(include(`"quoted"`)), "quotes are JSON-escaped in Other")
	assert.Equal(t, []string{"quoted"}, findRequestIds(include("<b>&")), "HTML characters are JSON-escaped in Other")
	assert.Equal(t, []string{"comma"}, findRequestIds(include("a,b")))
	assert.Empty(t, findRequestIds(include("%")), "percent is literal, not a wildcard")
	assert.Empty(t, findRequestIds(include("missing")))
	assert.Equal(t, []string{"decrypt", "quoted", "comma", "no-description", "elsewhere"}, findRequestIds(LogFilter{}), "empty filter changes nothing")
	assert.Equal(t, []string{"quoted", "comma", "no-description", "elsewhere"},
		findRequestIds(LogFilter{RetryRule: LogFieldFilter{Values: []string{"case1"}, Exclude: true}}))
}
