package model

import (
	"gorm.io/gorm"
)

// ErrorStatFilter narrows the error-log scan for the "明细错误" stats page. Type
// is always LogTypeError; the remaining fields are optional.
type ErrorStatFilter struct {
	StartTimestamp int64
	EndTimestamp   int64
	StatusCode     int
	ModelName      string
	Channel        int
	Group          string
}

// ErrorSignatureStat is one aggregated signature row (the headline grouping).
type ErrorSignatureStat struct {
	SignatureHash string `gorm:"column:error_signature_hash" json:"signature_hash"`
	Signature     string `gorm:"column:signature" json:"signature"`
	Count         int64  `gorm:"column:cnt" json:"count"`
	FirstSeen     int64  `gorm:"column:first_seen" json:"first_seen"`
	LastSeen      int64  `gorm:"column:last_seen" json:"last_seen"`
}

// ErrorSignatureDimCount is a per-signature breakdown bucket (by model or by
// channel). Only the relevant key column is populated per query.
type ErrorSignatureDimCount struct {
	SignatureHash string `gorm:"column:error_signature_hash" json:"signature_hash"`
	ModelName     string `gorm:"column:model_name" json:"model"`
	ChannelId     int    `gorm:"column:channel_id" json:"channel_id"`
	Count         int64  `gorm:"column:cnt" json:"count"`
}

func applyErrorStatFilter(tx *gorm.DB, f ErrorStatFilter) *gorm.DB {
	tx = tx.Where("type = ?", LogTypeError)
	if f.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", f.StartTimestamp)
	}
	if f.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", f.EndTimestamp)
	}
	if f.ModelName != "" {
		tx = tx.Where("model_name = ?", f.ModelName)
	}
	if f.Channel != 0 {
		tx = tx.Where("channel_id = ?", f.Channel)
	}
	if f.Group != "" {
		tx = tx.Where(logGroupCol+" = ?", f.Group)
	}
	if f.StatusCode != 0 {
		condition, args := statusCodeOtherCondition("other", f.StatusCode)
		tx = tx.Where(condition, args...)
	}
	return tx
}

// GetErrorSignatureStats returns the top-N error signatures by occurrence count
// within the filter window. Grouping is on the indexed error_signature_hash, so
// this is a cheap index-backed aggregation rather than a full content scan.
func GetErrorSignatureStats(f ErrorStatFilter, limit int) ([]*ErrorSignatureStat, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var rows []*ErrorSignatureStat
	err := applyErrorStatFilter(LOG_DB.Table("logs"), f).
		Select("error_signature_hash, MIN(error_signature) as signature, COUNT(*) as cnt, MIN(created_at) as first_seen, MAX(created_at) as last_seen").
		Group("error_signature_hash").
		Order("cnt DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}

// GetErrorSignatureModelBreakdown returns per-(signature, model) counts for the
// given signature hashes, honoring the same filter window.
func GetErrorSignatureModelBreakdown(f ErrorStatFilter, hashes []string) ([]*ErrorSignatureDimCount, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	var rows []*ErrorSignatureDimCount
	err := applyErrorStatFilter(LOG_DB.Table("logs"), f).
		Where("error_signature_hash IN ?", hashes).
		Select("error_signature_hash, model_name, COUNT(*) as cnt").
		Group("error_signature_hash, model_name").
		Scan(&rows).Error
	return rows, err
}

// GetErrorSignatureChannelBreakdown returns per-(signature, channel) counts for
// the given signature hashes, honoring the same filter window.
func GetErrorSignatureChannelBreakdown(f ErrorStatFilter, hashes []string) ([]*ErrorSignatureDimCount, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	var rows []*ErrorSignatureDimCount
	err := applyErrorStatFilter(LOG_DB.Table("logs"), f).
		Where("error_signature_hash IN ?", hashes).
		Select("error_signature_hash, channel_id, COUNT(*) as cnt").
		Group("error_signature_hash, channel_id").
		Scan(&rows).Error
	return rows, err
}
