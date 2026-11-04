package model

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// logSessionNotFound is the session_id sentinel the relay writes for a
// session-capable request without a resolvable id (see relay/session_id.go).
// It is not a real conversation, so the session table skips it.
const logSessionNotFound = "notfound"

// logSessionScanLimit caps the per-page logs scan used for the interval
// statistics; the totals from log_sessions are unaffected by it.
const logSessionScanLimit = 200000

// log_sessions holds one aggregate row per (session_id, user_id). It exists
// only on a ClickHouse log database: the log_sessions_mv materialized view
// folds every consume / error log with a real session id into it on insert,
// so no relay code writes to it. Rows merge in the background; readers always
// re-aggregate with GROUP BY.
const logSessionsCreateTableSQL = `CREATE TABLE IF NOT EXISTS log_sessions (
	session_id String,
	user_id Int32,
	request_count SimpleAggregateFunction(sum, UInt64),
	consume_count SimpleAggregateFunction(sum, UInt64),
	error_count SimpleAggregateFunction(sum, UInt64),
	first_at SimpleAggregateFunction(min, Int64),
	last_at SimpleAggregateFunction(max, Int64),
	quota SimpleAggregateFunction(sum, Int64),
	prompt_tokens SimpleAggregateFunction(sum, Int64),
	completion_tokens SimpleAggregateFunction(sum, Int64),
	cache_read SimpleAggregateFunction(sum, Int64),
	cache_input SimpleAggregateFunction(sum, Int64),
	cache_hit SimpleAggregateFunction(sum, UInt64),
	cache_cold_write SimpleAggregateFunction(sum, UInt64),
	usernames SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
	token_names SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
	model_names SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
	group_names SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
	channel_ids SimpleAggregateFunction(groupUniqArrayArray, Array(Int32)),
	customer_names SimpleAggregateFunction(groupUniqArrayArray, Array(String)),
	customer_ids SimpleAggregateFunction(groupUniqArrayArray, Array(String))
) ENGINE = AggregatingMergeTree()
ORDER BY (session_id, user_id)`

// logSessionRowSelect turns each logs row into one log_sessions row. The cache
// columns follow classifyCacheRow: Claude's prompt_tokens excludes cache read
// and creation, so they are added back into the input total. Source columns
// are qualified so the output aliases never shadow them.
const logSessionRowSelect = `SELECT
	logs.session_id AS session_id,
	logs.user_id AS user_id,
	toUInt64(1) AS request_count,
	toUInt64(logs.type = 2) AS consume_count,
	toUInt64(logs.type = 5) AS error_count,
	logs.created_at AS first_at,
	logs.created_at AS last_at,
	toInt64(logs.quota) AS quota,
	toInt64(logs.prompt_tokens) AS prompt_tokens,
	toInt64(logs.completion_tokens) AS completion_tokens,
	if(logs.type = 2, toInt64(JSONExtractFloat(logs.other, 'cache_tokens')), 0) AS cache_read,
	if(logs.type = 2, toInt64(logs.prompt_tokens) + if(JSONExtractBool(logs.other, 'claude'),
		toInt64(JSONExtractFloat(logs.other, 'cache_tokens')) + toInt64(JSONExtractFloat(logs.other, 'cache_creation_tokens')), 0), 0) AS cache_input,
	toUInt64(logs.type = 2 AND JSONExtractFloat(logs.other, 'cache_tokens') > 0) AS cache_hit,
	toUInt64(logs.type = 2 AND JSONExtractFloat(logs.other, 'cache_tokens') <= 0
		AND JSONExtractFloat(logs.other, 'cache_creation_tokens') > 0) AS cache_cold_write,
	if(logs.username = '', [], [logs.username]) AS usernames,
	if(logs.token_name = '', [], [logs.token_name]) AS token_names,
	if(logs.model_name = '', [], [logs.model_name]) AS model_names,
	if(logs.` + "`group`" + ` = '', [], [logs.` + "`group`" + `]) AS group_names,
	if(logs.channel_id = 0, [], [logs.channel_id]) AS channel_ids,
	if(logs.customer_name = '', [], [logs.customer_name]) AS customer_names,
	if(logs.customer_id = '', [], [logs.customer_id]) AS customer_ids
FROM logs
WHERE logs.type IN (2, 5) AND logs.session_id NOT IN ('', 'notfound')`

// logSessionsBackfillCreateTableSQL records the backfilled days so a second
// run never counts a day twice.
const logSessionsBackfillCreateTableSQL = `CREATE TABLE IF NOT EXISTS log_sessions_backfill (
	day_start Int64,
	done_at Int64
) ENGINE = MergeTree()
ORDER BY day_start`

// LogSessionsEnabled reports whether this deployment keeps the session table,
// which needs a ClickHouse log database.
func LogSessionsEnabled() bool {
	return common.UsingLogDatabase(common.DatabaseTypeClickHouse)
}

// MigrateLogSessions creates the session table, the materialized view feeding
// it and the backfill ledger. It must run after the logs columns the view
// reads exist. History is not loaded here; see StartLogSessionsBackfill.
func MigrateLogSessions() error {
	if !LogSessionsEnabled() {
		return nil
	}
	if err := LOG_DB.Exec(logSessionsCreateTableSQL).Error; err != nil {
		return err
	}
	if err := LOG_DB.Exec(logSessionsBackfillCreateTableSQL).Error; err != nil {
		return err
	}
	return LOG_DB.Exec("CREATE MATERIALIZED VIEW IF NOT EXISTS log_sessions_mv TO log_sessions AS " + logSessionRowSelect).Error
}

// LogSession is one row of the usage-log session view: the lifetime totals of
// a session from log_sessions. Intervals are the gaps (seconds) between
// consecutive consume logs, computed from logs for the listed page only, so
// error/retry attempts do not shrink them; they are 0 below two consume logs.
type LogSession struct {
	SessionId        string               `json:"session_id"`
	Customers        []LogSessionCustomer `json:"customers"`
	RequestCount     int64                `json:"request_count"`
	ConsumeCount     int64                `json:"consume_count"`
	ErrorCount       int64                `json:"error_count"`
	FirstAt          int64                `json:"first_at"`
	LastAt           int64                `json:"last_at"`
	Quota            int64                `json:"quota"`
	PromptTokens     int64                `json:"prompt_tokens"`
	CompletionTokens int64                `json:"completion_tokens"`
	TotalTokens      int64                `json:"total_tokens"`
	Username         string               `json:"username"`
	UserCount        int64                `json:"user_count"`
	TokenName        string               `json:"token_name"`
	TokenCount       int64                `json:"token_count"`
	ModelName        string               `json:"model_name"`
	ModelCount       int64                `json:"model_count"`
	AvgInterval      float64              `json:"avg_interval"`
	MinInterval      int64                `json:"min_interval"`
	MaxInterval      int64                `json:"max_interval"`
	Cache            LogCacheCell         `json:"cache"`
	// Truncated reports that the interval scan hit logSessionScanLimit, so the
	// intervals of this page cover only part of the sessions.
	Truncated bool `json:"truncated,omitempty"`
}

type LogSessionCustomer struct {
	CustomerId string `json:"customer_id"`
	Name       string `json:"name,omitempty"`
}

// logSessionAggRow is one re-aggregated log_sessions row. The s_ aliases keep
// ClickHouse from resolving an aggregate's argument to its own alias.
type logSessionAggRow struct {
	SessionId        string   `gorm:"column:session_id"`
	RequestCount     uint64   `gorm:"column:s_request_count"`
	ConsumeCount     uint64   `gorm:"column:s_consume_count"`
	ErrorCount       uint64   `gorm:"column:s_error_count"`
	FirstAt          int64    `gorm:"column:s_first_at"`
	LastAt           int64    `gorm:"column:s_last_at"`
	Quota            int64    `gorm:"column:s_quota"`
	PromptTokens     int64    `gorm:"column:s_prompt_tokens"`
	CompletionTokens int64    `gorm:"column:s_completion_tokens"`
	CacheRead        int64    `gorm:"column:s_cache_read"`
	CacheInput       int64    `gorm:"column:s_cache_input"`
	CacheHit         uint64   `gorm:"column:s_cache_hit"`
	CacheColdWrite   uint64   `gorm:"column:s_cache_cold_write"`
	UserCount        uint64   `gorm:"column:s_user_count"`
	Username         string   `gorm:"column:s_username"`
	TokenName        string   `gorm:"column:s_token_name"`
	TokenCount       uint64   `gorm:"column:s_token_count"`
	ModelName        string   `gorm:"column:s_model_name"`
	ModelCount       uint64   `gorm:"column:s_model_count"`
	CustomerIds      []string `gorm:"column:s_customer_ids"`
}

const logSessionAggSelect = `SELECT
	session_id,
	sum(request_count) AS s_request_count,
	sum(consume_count) AS s_consume_count,
	sum(error_count) AS s_error_count,
	min(first_at) AS s_first_at,
	max(last_at) AS s_last_at,
	sum(quota) AS s_quota,
	sum(prompt_tokens) AS s_prompt_tokens,
	sum(completion_tokens) AS s_completion_tokens,
	sum(cache_read) AS s_cache_read,
	sum(cache_input) AS s_cache_input,
	sum(cache_hit) AS s_cache_hit,
	sum(cache_cold_write) AS s_cache_cold_write,
	uniqExact(user_id) AS s_user_count,
	arraySort(groupUniqArrayArray(usernames)) AS s_usernames,
	arraySort(groupUniqArrayArray(token_names)) AS s_token_names,
	arraySort(groupUniqArrayArray(model_names)) AS s_model_names,
	groupUniqArrayArray(group_names) AS s_group_names,
	groupUniqArrayArray(channel_ids) AS s_channel_ids,
	groupUniqArrayArray(customer_names) AS s_customer_names,
	arraySort(groupUniqArrayArray(customer_ids)) AS s_customer_ids,
	arrayElement(s_usernames, 1) AS s_username,
	arrayElement(s_token_names, 1) AS s_token_name,
	length(s_token_names) AS s_token_count,
	arrayElement(s_model_names, 1) AS s_model_name,
	length(s_model_names) AS s_model_count
FROM log_sessions`

// logSessionArrayCondition matches a session's value set against an
// include/exclude field filter: include keeps sessions with any element equal
// to (or, with allowLike and a % wildcard, LIKE) any value; exclude keeps
// sessions with no such element. It returns "" for an empty filter.
func logSessionArrayCondition(column string, filter LogFieldFilter, allowLike bool) (string, []any, error) {
	conditions := make([]string, 0, len(filter.Values))
	args := make([]any, 0, len(filter.Values))
	for _, value := range filter.Values {
		if value == "" {
			continue
		}
		if allowLike && strings.Contains(value, "%") {
			pattern, err := sanitizeClickHouseLikePattern(value)
			if err != nil {
				return "", nil, err
			}
			conditions = append(conditions, "arrayExists(x -> x LIKE ?, "+column+")")
			args = append(args, pattern)
			continue
		}
		conditions = append(conditions, "has("+column+", ?)")
		args = append(args, value)
	}
	if len(conditions) == 0 {
		return "", nil, nil
	}
	condition := "(" + strings.Join(conditions, " OR ") + ")"
	if filter.Exclude {
		condition = "NOT " + condition
	}
	return condition, args, nil
}

// logSessionArrayFilter pairs a re-aggregated value-set column with the log
// field filter applied to it.
type logSessionArrayFilter struct {
	column    string
	filter    LogFieldFilter
	allowLike bool
}

// logSessionQuery builds the grouped log_sessions query for filter. A session
// is listed when its lifetime [first_at, last_at] overlaps the filter's time
// range; only the session id and the dimensions the table keeps (user, token,
// model, group, channel, customer) filter it, every other log filter is
// ignored. userId > 0 limits the rows to that user.
func logSessionQuery(userId int, filter LogFilter) (string, []any, error) {
	var where []string
	var args []any
	if userId > 0 {
		where = append(where, "user_id = ?")
		args = append(args, userId)
	}
	if filter.SessionId != "" {
		where = append(where, "session_id = ?")
		args = append(args, filter.SessionId)
	}

	var having []string
	var havingArgs []any
	if filter.StartTimestamp != 0 {
		having = append(having, "s_last_at >= ?")
		havingArgs = append(havingArgs, filter.StartTimestamp)
	}
	if filter.EndTimestamp != 0 {
		having = append(having, "s_first_at <= ?")
		havingArgs = append(havingArgs, filter.EndTimestamp)
	}
	arrayFilters := []logSessionArrayFilter{
		{"s_model_names", filter.ModelName, true},
		{"s_usernames", filter.Username, true},
		{"s_token_names", filter.TokenName, false},
		{"s_group_names", filter.Group, false},
	}
	if filter.CustomerName != "" {
		arrayFilters = append(arrayFilters, logSessionArrayFilter{"s_customer_names", LogFieldFilter{Values: []string{filter.CustomerName}}, false})
	}
	if filter.CustomerId != "" {
		arrayFilters = append(arrayFilters, logSessionArrayFilter{"s_customer_ids", LogFieldFilter{Values: []string{filter.CustomerId}}, false})
	}
	for _, f := range arrayFilters {
		condition, conditionArgs, err := logSessionArrayCondition(f.column, f.filter, f.allowLike)
		if err != nil {
			return "", nil, err
		}
		if condition != "" {
			having = append(having, condition)
			havingArgs = append(havingArgs, conditionArgs...)
		}
	}
	if len(filter.Channel.Values) > 0 {
		channelConditions := make([]string, 0, len(filter.Channel.Values))
		for _, id := range filter.Channel.Values {
			channelConditions = append(channelConditions, "has(s_channel_ids, ?)")
			havingArgs = append(havingArgs, int32(id))
		}
		condition := "(" + strings.Join(channelConditions, " OR ") + ")"
		if filter.Channel.Exclude {
			condition = "NOT " + condition
		}
		having = append(having, condition)
	}

	var sql strings.Builder
	sql.WriteString(logSessionAggSelect)
	if len(where) > 0 {
		sql.WriteString(" WHERE " + strings.Join(where, " AND "))
	}
	sql.WriteString(" GROUP BY session_id")
	if len(having) > 0 {
		sql.WriteString(" HAVING " + strings.Join(having, " AND "))
	}
	return sql.String(), append(args, havingArgs...), nil
}

// LogSessionSortOptions orders the session list. SortBy "" (the default)
// keeps the original newest-activity-first order; "total_tokens" sorts by the
// session's lifetime prompt+completion tokens.
type LogSessionSortOptions struct {
	SortBy    string
	SortOrder string
}

// logSessionSortColumns maps a validated SortBy to the aggregated expression
// it orders by, referencing the outer SELECT's own aliases.
var logSessionSortColumns = map[string]string{
	"total_tokens": "s_prompt_tokens + s_completion_tokens",
}

func NewLogSessionSortOptions(sortBy string, sortOrder string) LogSessionSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := logSessionSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = ""
		normalizedSortOrder = ""
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}
	return LogSessionSortOptions{SortBy: normalizedSortBy, SortOrder: normalizedSortOrder}
}

func (options LogSessionSortOptions) orderByClause() string {
	columnExpr, ok := logSessionSortColumns[options.SortBy]
	if !ok {
		return " ORDER BY s_last_at DESC, session_id DESC"
	}
	direction := "DESC"
	if options.SortOrder == "asc" {
		direction = "ASC"
	}
	return fmt.Sprintf(" ORDER BY (%s) %s, session_id DESC", columnExpr, direction)
}

// logSessionTotals is the count and token sum over every session matching the
// filter, independent of the page being listed.
type logSessionTotals struct {
	Total       int64 `gorm:"column:total"`
	TotalTokens int64 `gorm:"column:total_tokens"`
}

// GetLogSessions lists sessions ordered by sortOptions (newest activity first
// by default) from log_sessions. userId > 0 limits the result to that user's
// requests. The returned token sum covers every session matching filter, not
// only the listed page.
func GetLogSessions(userId int, filter LogFilter, startIdx int, num int, sortOptions LogSessionSortOptions) ([]*LogSession, int64, int64, error) {
	query, args, err := logSessionQuery(userId, filter)
	if err != nil {
		return nil, 0, 0, err
	}
	var totals logSessionTotals
	totalsSQL := "SELECT count() AS total, sum(s_prompt_tokens + s_completion_tokens) AS total_tokens FROM (" + query + ")"
	if err := LOG_DB.Raw(totalsSQL, args...).Scan(&totals).Error; err != nil {
		return nil, 0, 0, err
	}
	sessions := make([]*LogSession, 0, num)
	if totals.Total == 0 {
		return sessions, 0, 0, nil
	}

	var rows []logSessionAggRow
	pageArgs := append(slices.Clone(args), num, startIdx)
	if err := LOG_DB.Raw(query+sortOptions.orderByClause()+" LIMIT ? OFFSET ?", pageArgs...).Scan(&rows).Error; err != nil {
		return nil, 0, 0, err
	}
	customerIDs := make([]string, 0)
	for _, row := range rows {
		customerIDs = append(customerIDs, row.CustomerIds...)
	}
	slices.Sort(customerIDs)
	customerIDs = slices.Compact(customerIDs)
	customerNames := make(map[string]string)
	if len(customerIDs) > 0 {
		var customers []Customer
		if err := DB.Select("customer_id", "name").Where("customer_id IN ?", customerIDs).Find(&customers).Error; err != nil {
			return nil, 0, 0, err
		}
		for _, customer := range customers {
			customerNames[customer.CustomerId] = customer.Name
		}
	}
	for _, r := range rows {
		sessionCustomers := make([]LogSessionCustomer, 0, len(r.CustomerIds))
		for _, customerID := range r.CustomerIds {
			sessionCustomers = append(sessionCustomers, LogSessionCustomer{
				CustomerId: customerID,
				Name:       customerNames[customerID],
			})
		}
		s := &LogSession{
			SessionId:        r.SessionId,
			Customers:        sessionCustomers,
			RequestCount:     int64(r.RequestCount),
			ConsumeCount:     int64(r.ConsumeCount),
			ErrorCount:       int64(r.ErrorCount),
			FirstAt:          r.FirstAt,
			LastAt:           r.LastAt,
			Quota:            r.Quota,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalTokens:      r.PromptTokens + r.CompletionTokens,
			Username:         r.Username,
			UserCount:        int64(r.UserCount),
			TokenName:        r.TokenName,
			TokenCount:       int64(r.TokenCount),
			ModelName:        r.ModelName,
			ModelCount:       int64(r.ModelCount),
			Cache: LogCacheCell{
				GrossRate: logCacheHitRate(r.CacheRead, r.CacheInput),
				CacheRead: r.CacheRead,
				InputAll:  r.CacheInput,
				Hit:       int(r.CacheHit),
				ColdWrite: int(r.CacheColdWrite),
			},
		}
		sessions = append(sessions, s)
	}
	if len(sessions) == 0 {
		return sessions, totals.Total, totals.TotalTokens, nil
	}
	if err := fillLogSessionIntervals(userId, sessions); err != nil {
		return nil, 0, 0, err
	}
	return sessions, totals.Total, totals.TotalTokens, nil
}

// logSessionTimeRow holds the columns the interval statistics need.
type logSessionTimeRow struct {
	SessionId string `gorm:"column:session_id"`
	CreatedAt int64  `gorm:"column:created_at"`
}

// fillLogSessionIntervals computes the request intervals of each listed
// session from its consume logs, served by the session_id bloom-filter index
// on logs. Logs before the session's first_at are skipped so the intervals
// cover the same requests as the table totals (history not yet backfilled
// stays out of both).
func fillLogSessionIntervals(userId int, sessions []*LogSession) error {
	ids := make([]string, 0, len(sessions))
	firstAt := make(map[string]int64, len(sessions))
	earliest := sessions[0].FirstAt
	for _, s := range sessions {
		ids = append(ids, s.SessionId)
		firstAt[s.SessionId] = s.FirstAt
		earliest = min(earliest, s.FirstAt)
	}
	tx := LOG_DB.Model(&Log{}).Where("logs.type = ? AND logs.session_id IN ? AND logs.created_at >= ?", LogTypeConsume, ids, earliest)
	if userId > 0 {
		tx = tx.Where("logs.user_id = ?", userId)
	}
	rows, err := tx.Select("logs.session_id, logs.created_at").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	times := make(map[string][]int64, len(sessions))
	truncated := false
	for scanned := 0; rows.Next(); scanned++ {
		if scanned >= logSessionScanLimit {
			truncated = true
			break
		}
		var r logSessionTimeRow
		if err := LOG_DB.ScanRows(rows, &r); err != nil {
			return err
		}
		if r.CreatedAt >= firstAt[r.SessionId] {
			times[r.SessionId] = append(times[r.SessionId], r.CreatedAt)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, s := range sessions {
		s.Truncated = truncated
		ts := times[s.SessionId]
		if len(ts) < 2 {
			continue
		}
		slices.Sort(ts)
		s.MinInterval = ts[1] - ts[0]
		for i := 1; i < len(ts); i++ {
			gap := ts[i] - ts[i-1]
			s.MinInterval = min(s.MinInterval, gap)
			s.MaxInterval = max(s.MaxInterval, gap)
		}
		s.AvgInterval = float64(ts[len(ts)-1]-ts[0]) / float64(len(ts)-1)
	}
	return nil
}

// logSessionsBackfillInsertSQL loads one time slice of history into
// log_sessions, pre-aggregated per (session_id, user_id). Columns follow the
// table order. Thread and memory limits keep the scan from starving the live
// log writes and reads.
const logSessionsBackfillInsertSQL = `INSERT INTO log_sessions
SELECT session_id, user_id,
	sum(request_count), sum(consume_count), sum(error_count),
	min(first_at), max(last_at),
	sum(quota), sum(prompt_tokens), sum(completion_tokens),
	sum(cache_read), sum(cache_input), sum(cache_hit), sum(cache_cold_write),
	groupUniqArrayArray(usernames), groupUniqArrayArray(token_names),
	groupUniqArrayArray(model_names), groupUniqArrayArray(group_names),
	groupUniqArrayArray(channel_ids), groupUniqArrayArray(customer_names),
	groupUniqArrayArray(customer_ids)
FROM (` + logSessionRowSelect + ` AND logs.created_at >= ? AND logs.created_at < ?)
GROUP BY session_id, user_id
SETTINGS max_threads = 2, max_memory_usage = 4000000000`

// logSessionsBackfillPause separates two backfilled days.
var logSessionsBackfillPause = 2 * time.Second

// LogSessionsBackfillStatus reports the backfill run of this process.
type LogSessionsBackfillStatus struct {
	Running bool `json:"running"`
	// Cutoff is when log_sessions_mv was created: logs before it are loaded
	// by the backfill, logs after it reached the table through the view.
	Cutoff    int64   `json:"cutoff"`
	Days      int     `json:"days"`
	DoneDays  []int64 `json:"done_days"`
	Skipped   []int64 `json:"skipped_days"`
	LastError string  `json:"last_error,omitempty"`
}

var (
	logSessionsBackfillMu    sync.Mutex
	logSessionsBackfillState LogSessionsBackfillStatus
)

// GetLogSessionsBackfillStatus returns a copy of this process's backfill state.
func GetLogSessionsBackfillStatus() LogSessionsBackfillStatus {
	logSessionsBackfillMu.Lock()
	defer logSessionsBackfillMu.Unlock()
	status := logSessionsBackfillState
	status.DoneDays = slices.Clone(status.DoneDays)
	status.Skipped = slices.Clone(status.Skipped)
	return status
}

// StartLogSessionsBackfill loads the last days of logs before the view's
// cutoff into log_sessions in the background, newest day first, one local
// calendar day per INSERT. Days already in log_sessions_backfill are skipped,
// so running it again never counts a day twice. It is started by an operator
// at a quiet hour; one run per process at a time.
func StartLogSessionsBackfill(days int) error {
	if !LogSessionsEnabled() {
		return common.NewMessage("Session view requires a ClickHouse log database")
	}
	var cutoff int64
	if err := LOG_DB.Raw("SELECT toInt64(toUnixTimestamp(metadata_modification_time)) FROM system.tables WHERE database = currentDatabase() AND name = 'log_sessions_mv'").Scan(&cutoff).Error; err != nil {
		return err
	}
	if cutoff == 0 {
		return common.NewMessage("Session table is not created yet")
	}

	logSessionsBackfillMu.Lock()
	if logSessionsBackfillState.Running {
		logSessionsBackfillMu.Unlock()
		return common.NewMessage("A session backfill is already running")
	}
	logSessionsBackfillState = LogSessionsBackfillStatus{Running: true, Cutoff: cutoff, Days: days}
	logSessionsBackfillMu.Unlock()

	go runLogSessionsBackfill(cutoff, days)
	return nil
}

func runLogSessionsBackfill(cutoff int64, days int) {
	finish := func(err error) {
		logSessionsBackfillMu.Lock()
		defer logSessionsBackfillMu.Unlock()
		logSessionsBackfillState.Running = false
		if err != nil {
			logSessionsBackfillState.LastError = err.Error()
			common.SysError(common.LogText("session backfill failed: %s", err.Error()))
		}
	}

	var done []int64
	if err := LOG_DB.Raw("SELECT day_start FROM log_sessions_backfill").Scan(&done).Error; err != nil {
		finish(err)
		return
	}
	cutoffTime := time.Unix(cutoff, 0)
	lastDay := time.Date(cutoffTime.Year(), cutoffTime.Month(), cutoffTime.Day(), 0, 0, 0, 0, time.Local)
	for i := range days {
		dayStart := lastDay.AddDate(0, 0, -i)
		start := dayStart.Unix()
		end := min(dayStart.AddDate(0, 0, 1).Unix(), cutoff)
		if slices.Contains(done, start) {
			logSessionsBackfillMu.Lock()
			logSessionsBackfillState.Skipped = append(logSessionsBackfillState.Skipped, start)
			logSessionsBackfillMu.Unlock()
			continue
		}
		if err := LOG_DB.Exec(logSessionsBackfillInsertSQL, start, end).Error; err != nil {
			finish(fmt.Errorf("day %s: %w", dayStart.Format("2006-01-02"), err))
			return
		}
		if err := LOG_DB.Exec("INSERT INTO log_sessions_backfill (day_start, done_at) VALUES (?, ?)", start, common.GetTimestamp()).Error; err != nil {
			finish(err)
			return
		}
		logSessionsBackfillMu.Lock()
		logSessionsBackfillState.DoneDays = append(logSessionsBackfillState.DoneDays, start)
		logSessionsBackfillMu.Unlock()
		time.Sleep(logSessionsBackfillPause)
	}
	finish(nil)
}
