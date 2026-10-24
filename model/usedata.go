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
	CustomerId string `json:"customer_id" gorm:"index;size:64;default:''"`
	TokenUsed  int    `json:"token_used" gorm:"default:0"`
	Count      int    `json:"count" gorm:"default:0"`
	Quota      int    `json:"quota" gorm:"default:0"`
	// ChannelStatQuota / CustomerStatQuota are the reporting-only stat amounts
	// (channel stat-group-ratio discounted / per-customer discounted), summed
	// alongside the real Quota so the dashboard chart can switch calibers.
	ChannelStatQuota  int `json:"channel_stat_quota" gorm:"default:0"`
	CustomerStatQuota int `json:"customer_stat_quota" gorm:"default:0"`
}

type QuotaDataLogParams struct {
	UserID            int
	Username          string
	ModelName         string
	Quota             int
	CreatedAt         int64
	TokenUsed         int
	UseGroup          string
	TokenID           int
	ChannelID         int
	NodeName          string
	CustomerId        string
	ChannelStatQuota  int
	CustomerStatQuota int
}

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
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
		UserID:     params.UserID,
		Username:   params.Username,
		ModelName:  params.ModelName,
		CreatedAt:  createdAt,
		UseGroup:   params.UseGroup,
		TokenID:    params.TokenID,
		ChannelID:  params.ChannelID,
		NodeName:   params.NodeName,
		CustomerId: params.CustomerId,
		Count:      1,
		Quota:      params.Quota,
		TokenUsed:  params.TokenUsed,

		ChannelStatQuota:  params.ChannelStatQuota,
		CustomerStatQuota: params.CustomerStatQuota,
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
	// 1. 先查询数据库中是否有数据
	// 2. 如果有数据，就更新数据
	// 3. 如果没有数据，就插入数据
	for _, quotaData := range CacheQuotaData {
		quotaDataDB := &QuotaData{}
		err := DB.Table("quota_data").
			Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ? and customer_id = ?",
				quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName, quotaData.CustomerId).
			First(quotaDataDB).Error
		// ErrRecordNotFound 是正常的“新桶”情况，走插入；其余是真正的数据库错误，
		// 此时行是否存在未知，贸然 Create 可能重复计数，记录并跳过本条。
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			failed++
			common.SysError(fmt.Sprintf("SaveQuotaDataCache query error: %s", err))
			continue
		}
		if quotaDataDB.Id > 0 {
			if err := increaseQuotaData(quotaData); err != nil {
				failed++
				continue
			}
		} else if err := DB.Table("quota_data").Create(quotaData).Error; err != nil {
			failed++
			common.SysError(fmt.Sprintf("SaveQuotaDataCache insert error: %s", err))
			continue
		}
		saved++
	}
	CacheQuotaData = make(map[string]*QuotaData)
	if failed > 0 {
		common.SysError(fmt.Sprintf("保存数据看板数据：成功 %d 条，失败 %d 条（共 %d 条）", saved, failed, total))
	} else {
		common.SysLog(fmt.Sprintf("保存数据看板数据成功，共保存%d条数据", saved))
	}
}

func increaseQuotaData(quotaData *QuotaData) error {
	err := DB.Table("quota_data").
		Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ? and customer_id = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt, quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName, quotaData.CustomerId).
		Updates(map[string]interface{}{
			"count":               gorm.Expr("count + ?", quotaData.Count),
			"quota":               gorm.Expr("quota + ?", quotaData.Quota),
			"token_used":          gorm.Expr("token_used + ?", quotaData.TokenUsed),
			"channel_stat_quota":  gorm.Expr("channel_stat_quota + ?", quotaData.ChannelStatQuota),
			"customer_stat_quota": gorm.Expr("customer_stat_quota + ?", quotaData.CustomerStatQuota),
		}).Error
	if err != nil {
		common.SysError(fmt.Sprintf("increaseQuotaData error: %s", err))
	}
	return err
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(channel_stat_quota) as channel_stat_quota, sum(customer_stat_quota) as customer_stat_quota").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	// 从quota_data表中查询数据
	err = DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(channel_stat_quota) as channel_stat_quota, sum(customer_stat_quota) as customer_stat_quota").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	var quotaDatas []*QuotaData
	err = DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(customer_stat_quota) as customer_stat_quota").
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
	err = DB.Table("quota_data").Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, sum(channel_stat_quota) as channel_stat_quota, sum(customer_stat_quota) as customer_stat_quota, created_at").Where("created_at >= ? and created_at <= ?", startTime, endTime).Group("model_name, created_at").Find(&quotaDatas).Error
	return quotaDatas, err
}
