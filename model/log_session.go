package model

import (
	"slices"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// logSessionNotFound is the session_id sentinel the relay writes for a
// session-capable request without a resolvable id (see relay/session_id.go).
// It is not a real conversation, so the session view skips it.
const logSessionNotFound = "notfound"

// logSessionScanLimit caps the per-page detail scan used for interval and
// cache statistics; the grouped totals are unaffected by it.
const logSessionScanLimit = 200000

// LogSession is one row of the usage-log session view: every consume / error
// log sharing a session_id within the filtered time range. Intervals are the
// gaps (seconds) between consecutive consume logs ordered by created_at, so
// error/retry attempts do not shrink them; they are 0 below two consume logs.
type LogSession struct {
	SessionId        string       `json:"session_id"`
	RequestCount     int64        `json:"request_count"`
	ConsumeCount     int64        `json:"consume_count"`
	ErrorCount       int64        `json:"error_count"`
	FirstAt          int64        `json:"first_at"`
	LastAt           int64        `json:"last_at"`
	Quota            int64        `json:"quota"`
	PromptTokens     int64        `json:"prompt_tokens"`
	CompletionTokens int64        `json:"completion_tokens"`
	Username         string       `json:"username"`
	UserCount        int64        `json:"user_count"`
	TokenName        string       `json:"token_name"`
	TokenCount       int64        `json:"token_count"`
	ModelName        string       `json:"model_name"`
	ModelCount       int64        `json:"model_count"`
	AvgInterval      float64      `json:"avg_interval" gorm:"-"`
	MinInterval      int64        `json:"min_interval" gorm:"-"`
	MaxInterval      int64        `json:"max_interval" gorm:"-"`
	Cache            LogCacheCell `json:"cache" gorm:"-"`
	// Truncated reports that the detail scan hit logSessionScanLimit, so the
	// intervals and cache cell of this page cover only part of the session.
	Truncated bool `json:"truncated,omitempty" gorm:"-"`
}

// logSessionScope restricts tx to the logs that form sessions: consume and
// error logs carrying a real session id, plus the owning user for /self.
func logSessionScope(tx *gorm.DB, userId int) *gorm.DB {
	tx = tx.Where("logs.type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("logs.session_id <> ? AND logs.session_id <> ?", "", logSessionNotFound)
	if userId > 0 {
		tx = tx.Where("logs.user_id = ?", userId)
	}
	return tx
}

// GetLogSessions groups the logs matching filter by session_id, newest
// activity first. userId > 0 limits the result to that user's logs. The log
// type filter is ignored: a session always covers its consume and error logs.
func GetLogSessions(userId int, filter LogFilter, startIdx int, num int) ([]*LogSession, int64, error) {
	filter.Type = LogTypeUnknown
	base, err := filter.apply(logSessionScope(LOG_DB.Model(&Log{}), userId))
	if err != nil {
		return nil, 0, err
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Distinct("logs.session_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	sessions := make([]*LogSession, 0, num)
	if total == 0 {
		return sessions, 0, nil
	}

	err = base.Session(&gorm.Session{}).
		Select(`logs.session_id AS session_id,
			COUNT(*) AS request_count,
			SUM(CASE WHEN logs.type = ? THEN 1 ELSE 0 END) AS consume_count,
			SUM(CASE WHEN logs.type = ? THEN 1 ELSE 0 END) AS error_count,
			MIN(logs.created_at) AS first_at,
			MAX(logs.created_at) AS last_at,
			SUM(logs.quota) AS quota,
			SUM(logs.prompt_tokens) AS prompt_tokens,
			SUM(logs.completion_tokens) AS completion_tokens,
			MAX(logs.username) AS username,
			COUNT(DISTINCT logs.user_id) AS user_count,
			MAX(logs.token_name) AS token_name,
			COUNT(DISTINCT logs.token_id) AS token_count,
			MAX(logs.model_name) AS model_name,
			COUNT(DISTINCT logs.model_name) AS model_count`, LogTypeConsume, LogTypeError).
		Group("logs.session_id").
		Order("last_at DESC, session_id DESC").
		Limit(num).Offset(startIdx).
		Scan(&sessions).Error
	if err != nil {
		return nil, 0, err
	}
	if len(sessions) == 0 {
		return sessions, total, nil
	}

	if err := fillLogSessionDetails(base, sessions); err != nil {
		return nil, 0, err
	}
	return sessions, total, nil
}

// logSessionScanRow holds the columns the interval and cache statistics need.
type logSessionScanRow struct {
	SessionId    string `gorm:"column:session_id"`
	CreatedAt    int64  `gorm:"column:created_at"`
	ModelName    string `gorm:"column:model_name"`
	PromptTokens int    `gorm:"column:prompt_tokens"`
	Other        string `gorm:"column:other"`
}

// fillLogSessionDetails computes the request intervals and the cache-hit cell
// of each listed session from its consume logs. The cache cell uses the same
// classification and denominators as the log-analysis popup.
func fillLogSessionDetails(base *gorm.DB, sessions []*LogSession) error {
	ids := make([]string, 0, len(sessions))
	for _, s := range sessions {
		ids = append(ids, s.SessionId)
	}
	rows, err := base.Session(&gorm.Session{}).
		Where("logs.type = ? AND logs.session_id IN ?", LogTypeConsume, ids).
		Select("logs.session_id, logs.created_at, logs.model_name, logs.prompt_tokens, logs.other").
		Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	times := make(map[string][]int64, len(sessions))
	caches := make(map[string]*cacheChannelAgg, len(sessions))
	truncated := false
	for scanned := 0; rows.Next(); scanned++ {
		if scanned >= logSessionScanLimit {
			truncated = true
			break
		}
		var r logSessionScanRow
		if err := LOG_DB.ScanRows(rows, &r); err != nil {
			return err
		}
		times[r.SessionId] = append(times[r.SessionId], r.CreatedAt)
		var o logAnalysisOther
		if r.Other != "" {
			_ = common.UnmarshalJsonStr(r.Other, &o)
		}
		agg := caches[r.SessionId]
		if agg == nil {
			agg = &cacheChannelAgg{}
			caches[r.SessionId] = agg
		}
		classifyCacheRow(agg, r.PromptTokens, int64(o.CacheTokens), int64(o.CacheCreationTokens), int64(cacheMinTokensFor(r.ModelName)), o.Claude)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, s := range sessions {
		s.Truncated = truncated
		if agg := caches[s.SessionId]; agg != nil {
			s.Cache = cacheCellFromAgg(agg)
		}
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
