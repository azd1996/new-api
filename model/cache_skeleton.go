package model

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

// CacheSkeleton is one outbound request reduced to its prompt-cache structure:
// per content block a hash, kind, name and byte length, plus the cache
// breakpoints (Claude) or cache routing key (OpenAI) and the upstream cache
// usage. It never stores prompt text; comparing consecutive skeletons of one
// session shows where the cached prefix diverged. ClickHouse-only (see
// MigrateCacheSkeletons).
type CacheSkeleton struct {
	Ts        time.Time `json:"ts" gorm:"column:ts"`
	EndTs     time.Time `json:"end_ts" gorm:"column:end_ts"`
	RequestId string    `json:"request_id" gorm:"column:request_id"`
	Attempt   uint8     `json:"attempt" gorm:"column:attempt"`
	Status    string    `json:"status" gorm:"column:status"`
	Node      string    `json:"node" gorm:"column:node"`
	TokenId   uint32    `json:"token_id" gorm:"column:token_id"`
	SessionId string    `json:"session_id" gorm:"column:session_id"`
	Lane      uint64    `json:"lane,string" gorm:"column:lane"`
	ChannelId uint32    `json:"channel_id" gorm:"column:channel_id"`
	KeyIdx    uint16    `json:"key_idx" gorm:"column:key_idx"`
	Model     string    `json:"model" gorm:"column:model"`
	ParamFp   uint64    `json:"param_fp,string" gorm:"column:param_fp"`
	Thinking  string    `json:"thinking" gorm:"column:thinking"`
	Effort    string    `json:"effort" gorm:"column:effort"`
	// Provider is SkeletonProviderClaude or SkeletonProviderOpenAI; it selects
	// the cache model used by the session analysis.
	Provider string `json:"provider" gorm:"column:provider"`
	// CacheKeyHash hashes the OpenAI prompt_cache_key (0 when absent) and
	// Retention is its prompt_cache_retention.
	CacheKeyHash uint64 `json:"-" gorm:"column:cache_key_hash"`
	Retention    string `json:"retention" gorm:"column:retention"`
	// Array columns need an explicit type: without one GORM parses a slice
	// field as an association and rejects the model.
	BlkHash       []uint64 `json:"-" gorm:"column:blk_hash;type:Array(UInt64)"`
	BlkKind       []string `json:"blk_kind" gorm:"column:blk_kind;type:Array(LowCardinality(String))"`
	BlkName       []string `json:"blk_name" gorm:"column:blk_name;type:Array(LowCardinality(String))"`
	BlkLen        []uint32 `json:"blk_len" gorm:"column:blk_len;type:Array(UInt32)"`
	MsgStart      []uint16 `json:"msg_start" gorm:"column:msg_start;type:Array(UInt16)"`
	BpIdx         []uint16 `json:"bp_idx" gorm:"column:bp_idx;type:Array(UInt16)"`
	BpTtl         []string `json:"bp_ttl" gorm:"column:bp_ttl;type:Array(LowCardinality(String))"`
	Fixed         string   `json:"fixed" gorm:"column:fixed"`
	CacheRead     uint32   `json:"cache_read" gorm:"column:cache_read"`
	CacheCreation uint32   `json:"cache_creation" gorm:"column:cache_creation"`
	Input         uint32   `json:"input" gorm:"column:input"`
	// ParentRequestId holds the OpenAI previous_response_id: the cached
	// prefix then lives upstream and the skeleton only sees the new input.
	ParentRequestId string `json:"parent_request_id" gorm:"column:parent_request_id"`
	PrefixLen       uint16 `json:"-" gorm:"column:prefix_len"`
}

const (
	SkeletonProviderClaude = "claude"
	SkeletonProviderOpenAI = "openai"
)

func (CacheSkeleton) TableName() string { return "cache_skeletons" }

// CacheSkeletonEnabled reports whether skeletons are recorded: only when the
// log database is ClickHouse, and not switched off by CACHE_SKELETON_ENABLED.
func CacheSkeletonEnabled() bool {
	return LOG_DB != nil &&
		common.UsingLogDatabase(common.DatabaseTypeClickHouse) &&
		common.GetEnvOrDefaultBool("CACHE_SKELETON_ENABLED", true)
}

// MigrateCacheSkeletons creates the skeleton table on a ClickHouse log store.
// Rows expire after CACHE_SKELETON_TTL_DAYS (default 14) by dropping whole
// daily partitions; CACHE_SKELETON_STORAGE_POLICY optionally pins the table to
// a configured storage policy.
func MigrateCacheSkeletons() error {
	if !common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return nil
	}
	ttlDays := common.GetEnvOrDefault("CACHE_SKELETON_TTL_DAYS", 14)
	if ttlDays <= 0 {
		ttlDays = 14
	}
	settings := "ttl_only_drop_parts = 1"
	if policy := common.GetEnvOrDefaultString("CACHE_SKELETON_STORAGE_POLICY", ""); policy != "" && !strings.ContainsAny(policy, `'\`) {
		settings += ", storage_policy = '" + policy + "'"
	}
	err := LOG_DB.Exec(`CREATE TABLE IF NOT EXISTS cache_skeletons (
		ts DateTime64(3), end_ts DateTime64(3), request_id String, attempt UInt8,
		status LowCardinality(String), node LowCardinality(String),
		token_id UInt32, session_id String, lane UInt64,
		channel_id UInt32, key_idx UInt16, model LowCardinality(String),
		param_fp UInt64, thinking LowCardinality(String), effort LowCardinality(String),
		provider LowCardinality(String) DEFAULT 'claude',
		cache_key_hash UInt64 DEFAULT 0, retention LowCardinality(String) DEFAULT '',
		blk_hash Array(UInt64) CODEC(ZSTD(3)),
		blk_kind Array(LowCardinality(String)) CODEC(ZSTD(3)),
		blk_name Array(LowCardinality(String)) CODEC(ZSTD(3)),
		blk_len Array(UInt32) CODEC(ZSTD(3)),
		msg_start Array(UInt16) CODEC(ZSTD(3)),
		bp_idx Array(UInt16), bp_ttl Array(LowCardinality(String)),
		fixed LowCardinality(String),
		cache_read UInt32, cache_creation UInt32, input UInt32,
		parent_request_id String, prefix_len UInt16
	) ENGINE = MergeTree()
	PARTITION BY toYYYYMMDD(ts)
	ORDER BY (token_id, session_id, lane, ts)
	TTL toDateTime(ts) + INTERVAL ` + strconv.Itoa(ttlDays) + ` DAY
	SETTINGS ` + settings).Error
	if err != nil {
		return err
	}
	// Tables created before OpenAI skeletons were recorded hold Claude rows
	// only, so the provider default backfills them correctly.
	for _, column := range []string{
		"provider LowCardinality(String) DEFAULT 'claude'",
		"cache_key_hash UInt64 DEFAULT 0",
		"retention LowCardinality(String) DEFAULT ''",
	} {
		if err := LOG_DB.Exec("ALTER TABLE cache_skeletons ADD COLUMN IF NOT EXISTS " + column).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetCacheSkeletons returns every recorded attempt of one request, oldest first.
func GetCacheSkeletons(requestId string) ([]*CacheSkeleton, error) {
	rows := make([]*CacheSkeleton, 0)
	err := LOG_DB.Where("request_id = ?", requestId).Order("attempt ASC").Order("ts ASC").Find(&rows).Error
	return rows, err
}

const (
	cacheSkeletonQueueSize  = 4096
	cacheSkeletonBatchSize  = 500
	cacheSkeletonFlushEvery = 2 * time.Second
)

var (
	cacheSkeletonQueue   chan *CacheSkeleton
	cacheSkeletonOnce    sync.Once
	cacheSkeletonDropped atomic.Int64
)

// EnqueueCacheSkeleton hands a finished skeleton to the background batch
// writer. It never blocks the relay: when the queue is full the row is dropped
// and counted.
func EnqueueCacheSkeleton(row *CacheSkeleton) {
	cacheSkeletonOnce.Do(func() {
		cacheSkeletonQueue = make(chan *CacheSkeleton, cacheSkeletonQueueSize)
		go runCacheSkeletonWriter()
	})
	select {
	case cacheSkeletonQueue <- row:
	default:
		if cacheSkeletonDropped.Add(1)%1000 == 1 {
			logger.LogWarn(context.Background(), common.LogText("cache skeleton queue full, dropped %d rows so far", cacheSkeletonDropped.Load()))
		}
	}
}

func runCacheSkeletonWriter() {
	ticker := time.NewTicker(cacheSkeletonFlushEvery)
	defer ticker.Stop()
	batch := make([]*CacheSkeleton, 0, cacheSkeletonBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := LOG_DB.Create(&batch).Error; err != nil {
			logger.LogError(context.Background(), common.LogText("cache skeleton batch write failed (%d rows): %v", len(batch), err))
		}
		batch = batch[:0]
	}
	for {
		select {
		case row := <-cacheSkeletonQueue:
			batch = append(batch, row)
			if len(batch) >= cacheSkeletonBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
