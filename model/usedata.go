package model

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup  string `json:"use_group" gorm:"index;size:64;default:''"`
	TokenID   int    `json:"token_id" gorm:"index;default:0"`
	ChannelID int    `json:"channel_id" gorm:"index;default:0"`
	NodeName  string `json:"node_name" gorm:"index;size:64;default:''"`
	// CustomerId is the downstream customer id decoded from the trace-tag header
	// (empty for traffic without a customer). It is a grouping dimension so the
	// customer-statistics dashboard can rank/trend by customer. Rows written
	// before this column exists carry the empty-string default.
	CustomerId          string `json:"customer_id" gorm:"index;size:64;default:''"`
	TokenUsed           int    `json:"token_used" gorm:"default:0"`
	Count               int    `json:"count" gorm:"default:0"`
	Quota               int    `json:"quota" gorm:"default:0"`
	CacheReadTokens     int64  `json:"cache_read_tokens" gorm:"default:0"`
	CacheCreationTokens int64  `json:"cache_creation_tokens" gorm:"default:0"`
	// ChannelStatQuota / CustomerStatQuota are the reporting-only stat amounts
	// (channel stat-group-ratio discounted / per-customer discounted), summed
	// alongside the real Quota so the dashboard can switch calibers. Never billed.
	ChannelStatQuota  int `json:"channel_stat_quota" gorm:"default:0"`
	CustomerStatQuota int `json:"customer_stat_quota" gorm:"default:0"`
}

type QuotaDataLogParams struct {
	UserID              int
	Username            string
	ModelName           string
	Quota               int
	CreatedAt           int64
	TokenUsed           int
	CacheReadTokens     int64
	CacheCreationTokens int64
	UseGroup            string
	TokenID             int
	ChannelID           int
	NodeName            string
	CustomerId          string
	ChannelStatQuota    int
	CustomerStatQuota   int
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog(common.LogText("updating dashboard data..."))
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(quotaData *QuotaData) {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%d\x00%s\x00%d\x00%d\x00%s\x00%s",
		quotaData.UserID,
		quotaData.Username,
		quotaData.ModelName,
		quotaData.CreatedAt,
		quotaData.UseGroup,
		quotaData.TokenID,
		quotaData.ChannelID,
		quotaData.NodeName,
		quotaData.CustomerId,
	)
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		cachedQuotaData.CacheReadTokens += quotaData.CacheReadTokens
		cachedQuotaData.CacheCreationTokens += quotaData.CacheCreationTokens
		cachedQuotaData.ChannelStatQuota += quotaData.ChannelStatQuota
		cachedQuotaData.CustomerStatQuota += quotaData.CustomerStatQuota
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(params QuotaDataLogParams) {
	// 只精确到小时
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	quotaData := &QuotaData{
		UserID:              params.UserID,
		Username:            params.Username,
		ModelName:           params.ModelName,
		CreatedAt:           createdAt,
		UseGroup:            params.UseGroup,
		TokenID:             params.TokenID,
		ChannelID:           params.ChannelID,
		NodeName:            params.NodeName,
		CustomerId:          params.CustomerId,
		Count:               1,
		Quota:               params.Quota,
		TokenUsed:           params.TokenUsed,
		CacheReadTokens:     max(0, params.CacheReadTokens),
		CacheCreationTokens: max(0, params.CacheCreationTokens),
		ChannelStatQuota:    params.ChannelStatQuota,
		CustomerStatQuota:   params.CustomerStatQuota,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

func SaveQuotaDataCache() {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	total := len(CacheQuotaData)
	saved := 0
	failed := 0
	// 如果缓存中有数据，就保存到数据库中
	for _, quotaData := range CacheQuotaData {
		quotaDataDB := &QuotaData{}
		err := DB.Table("quota_data").
			Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ? and customer_id = ?",
				quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName, quotaData.CustomerId).
			First(quotaDataDB).Error
		// ErrRecordNotFound is the normal "new bucket" case -> insert; any other
		// error means row existence is unknown, so skip to avoid double counting.
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			failed++
			common.SysError(common.LogText("SaveQuotaDataCache query error: %s", err.Error()))
			continue
		}
		if quotaDataDB.Id > 0 {
			if err := increaseQuotaData(quotaData); err != nil {
				failed++
				continue
			}
		} else if err := DB.Table("quota_data").Create(quotaData).Error; err != nil {
			failed++
			common.SysError(common.LogText("SaveQuotaDataCache insert error: %s", err.Error()))
			continue
		}
		saved++
	}
	CacheQuotaData = make(map[string]*QuotaData)
	if failed > 0 {
		common.SysError(common.LogText("saved dashboard data: %d ok, %d failed (of %d)", saved, failed, total))
	} else {
		common.SysLog(common.LogText("saved dashboard data, %d records", saved))
	}
}

func increaseQuotaData(quotaData *QuotaData) error {
	err := DB.Table("quota_data").
		Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ? and customer_id = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName, quotaData.CustomerId).
		Updates(map[string]any{
			"count":                 gorm.Expr("count + ?", quotaData.Count),
			"quota":                 gorm.Expr("quota + ?", quotaData.Quota),
			"token_used":            gorm.Expr("token_used + ?", quotaData.TokenUsed),
			"cache_read_tokens":     gorm.Expr("cache_read_tokens + ?", quotaData.CacheReadTokens),
			"cache_creation_tokens": gorm.Expr("cache_creation_tokens + ?", quotaData.CacheCreationTokens),
			"channel_stat_quota":    gorm.Expr("channel_stat_quota + ?", quotaData.ChannelStatQuota),
			"customer_stat_quota":   gorm.Expr("customer_stat_quota + ?", quotaData.CustomerStatQuota),
		}).Error
	if err != nil {
		common.SysError(common.LogText("increaseQuotaData error: %s", err))
	}
	return err
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(cache_read_tokens) as cache_read_tokens, sum(cache_creation_tokens) as cache_creation_tokens, sum(channel_stat_quota) as channel_stat_quota, sum(customer_stat_quota) as customer_stat_quota").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(cache_read_tokens) as cache_read_tokens, sum(cache_creation_tokens) as cache_creation_tokens, sum(channel_stat_quota) as channel_stat_quota, sum(customer_stat_quota) as customer_stat_quota").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(cache_read_tokens) as cache_read_tokens, sum(cache_creation_tokens) as cache_creation_tokens, sum(customer_stat_quota) as customer_stat_quota").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

// GetQuotaDataGroupByCustomer aggregates per (customer_id, hour) for the
// customer-statistics dashboard. Only rows carrying a customer id are returned.
// Both channel-discounted and customer-discounted stat amounts are summed so the
// caller can fall back to the channel amount for customers that are not yet
// configured with a per-customer discount.
func GetQuotaDataGroupByCustomer(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("customer_id, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(channel_stat_quota) as channel_stat_quota, sum(customer_stat_quota) as customer_stat_quota").
		Where("created_at >= ? and created_at <= ? and customer_id <> ''", startTime, endTime).
		Group("customer_id, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsername(username, startTime, endTime)
	}
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	// only select model_name, sum(count) as count, sum(quota) as quota, model_name, created_at from quota_data group by model_name, created_at;
	//err = DB.Table("quota_data").Where("created_at >= ? and created_at <= ?", startTime, endTime).Find(&quotaDatas).Error
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(cache_read_tokens) as cache_read_tokens, sum(cache_creation_tokens) as cache_creation_tokens, sum(channel_stat_quota) as channel_stat_quota, sum(customer_stat_quota) as customer_stat_quota, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}
