package model

// CslHourly is the ClickHouse hourly aggregated call log written by offline jobs.
type CslHourly struct {
	StartTime              int64   `json:"start_time" gorm:"column:start_time"`
	EndTime                int64   `json:"end_time" gorm:"column:end_time"`
	UserId                 int     `json:"user_id" gorm:"column:user_id"`
	TokenId                int     `json:"token_id" gorm:"column:token_id"`
	ChannelId              int     `json:"channel_id" gorm:"column:channel_id"`
	TokenName              string  `json:"token_name" gorm:"column:token_name"`
	Username               string  `json:"username" gorm:"column:username"`
	GroupName              string  `json:"group_name" gorm:"column:group_name"`
	ModelName              string  `json:"model_name" gorm:"column:model_name"`
	StatType               string  `json:"stat_type" gorm:"column:stat_type"`
	CacheTtl               string  `json:"cache_ttl" gorm:"column:cache_ttl"`
	PricingTier            string  `json:"pricing_tier" gorm:"column:pricing_tier"`
	TokenBucket            string  `json:"token_bucket" gorm:"column:token_bucket"`
	CallCount              int     `json:"call_count" gorm:"column:call_count"`
	Tokens                 int64   `json:"tokens" gorm:"column:tokens"`
	UnitPriceUsdPerMillion float64 `json:"unit_price_usd_per_million" gorm:"column:unit_price_usd_per_million"`
	OriginalPriceUsd       float64 `json:"original_price_usd" gorm:"column:original_price_usd"`
	SettlementPriceUsd     float64 `json:"settlement_price_usd" gorm:"column:settlement_price_usd"`
	UsdExchangeRate        float64 `json:"usd_exchange_rate" gorm:"column:usd_exchange_rate"`
	UnitPriceCnyPerMillion float64 `json:"unit_price_cny_per_million" gorm:"column:unit_price_cny_per_million"`
	OriginalPriceCny       float64 `json:"original_price_cny" gorm:"column:original_price_cny"`
	SettlementPriceCny     float64 `json:"settlement_price_cny" gorm:"column:settlement_price_cny"`
	ChannelDiscount        float64 `json:"channel_discount" gorm:"column:channel_discount"`
}

func (CslHourly) TableName() string {
	return "csl_hourly"
}

type CslHourlyQuery struct {
	StartTimestamp int64
	EndTimestamp   int64
	GroupName      string
	Username       string
	ModelName      string
	TokenName      string
}

func GetCslHourly(params CslHourlyQuery) ([]*CslHourly, error) {
	rows := make([]*CslHourly, 0)
	query := LOG_DB.Table("csl_hourly").
		Where("start_time >= ? AND start_time <= ?", params.StartTimestamp, params.EndTimestamp)
	if params.GroupName != "" {
		query = query.Where("group_name = ?", params.GroupName)
	}
	if params.Username != "" {
		query = query.Where("username = ?", params.Username)
	}
	if params.ModelName != "" {
		query = query.Where("model_name = ?", params.ModelName)
	}
	if params.TokenName != "" {
		query = query.Where("token_name = ?", params.TokenName)
	}
	err := query.Order("start_time DESC").Find(&rows).Error
	return rows, err
}
