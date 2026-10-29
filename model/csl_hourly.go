package model

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
)

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

// GetCslHourly returns the hourly billing summary rows matching params.
//
// The data source is chosen by common.CslHourlyReaderEnabled: either LOG_DB's
// local csl_hourly table (default) or OmniDataSearch (see csl_hourly_omni.go).
// The two are mutually exclusive; there is no fallback between them, because a
// bill that silently depends on which backend answered is worse than an error.
func GetCslHourly(params CslHourlyQuery) ([]*CslHourly, error) {
	if common.CslHourlyReaderEnabled {
		return getCslHourlyFromOmni(params)
	}
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

// CslMonthlyQuery selects the hourly rows that a monthly per-user total is
// aggregated from. StartTimestamp/EndTimestamp bound the month (inclusive, in
// Unix seconds); GroupName scopes the result to one group the way the admin
// hourly query does.
type CslMonthlyQuery struct {
	StartTimestamp int64
	EndTimestamp   int64
	GroupName      string
	ModelName      string
}

// CslMonthlyUserTotal is one user's total settlement spend over the queried
// month, in both currencies the hourly rows carry.
type CslMonthlyUserTotal struct {
	UserId             int     `json:"user_id"`
	Username           string  `json:"username"`
	SettlementPriceCny float64 `json:"settlement_price_cny"`
	SettlementPriceUsd float64 `json:"settlement_price_usd"`
}

// cslMonthlyChunkSeconds is the width of one GetCslHourly window used to sweep a
// month. It is 168 hours so each window's enumerated start_time list on the
// OmniDataSearch path stays well under cslHourlyMaxHours, keeping each upstream
// IN clause modest regardless of how high that cap is raised.
const cslMonthlyChunkSeconds = int64(168 * 3600)

// CSL_MONTHLY_PLACEHOLDER

// GetCslMonthlyUserTotals returns each user's total settlement spend over the
// month described by params, summed per user.
//
// The month is swept in contiguous <=168-hour windows, each fetched through
// GetCslHourly, so the OmniDataSearch start_time IN clause stays small per
// request no matter how long the month is. Because a row's business key includes
// its hour-aligned start_time, every hour belongs to exactly one window;
// GetCslHourly already keeps only the newest rerun per business key within a
// window, so concatenating windows never double-counts a rerun and never splits
// one.
func GetCslMonthlyUserTotals(params CslMonthlyQuery) ([]*CslMonthlyUserTotal, error) {
	startHour := params.StartTimestamp - params.StartTimestamp%3600
	endHour := params.EndTimestamp - params.EndTimestamp%3600
	if startHour <= 0 || endHour < startHour {
		return []*CslMonthlyUserTotal{}, nil
	}

	totals := make(map[int]*CslMonthlyUserTotal)
	for winStart := startHour; winStart <= endHour; winStart += cslMonthlyChunkSeconds {
		winEnd := winStart + cslMonthlyChunkSeconds - 3600
		if winEnd > endHour {
			winEnd = endHour
		}
		rows, err := GetCslHourly(CslHourlyQuery{
			StartTimestamp: winStart,
			EndTimestamp:   winEnd,
			GroupName:      params.GroupName,
			ModelName:      params.ModelName,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			total, ok := totals[row.UserId]
			if !ok {
				total = &CslMonthlyUserTotal{UserId: row.UserId, Username: row.Username}
				totals[row.UserId] = total
			}
			total.SettlementPriceCny += row.SettlementPriceCny
			total.SettlementPriceUsd += row.SettlementPriceUsd
		}
	}

	result := make([]*CslMonthlyUserTotal, 0, len(totals))
	for _, total := range totals {
		result = append(result, total)
	}
	// Biggest spender first; username/user_id only make the order total so a
	// repeated query returns rows in one order.
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.SettlementPriceCny != b.SettlementPriceCny {
			return a.SettlementPriceCny > b.SettlementPriceCny
		}
		if a.Username != b.Username {
			return a.Username < b.Username
		}
		return a.UserId < b.UserId
	})
	return result, nil
}
