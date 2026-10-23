package model

import (
	"sort"

	"github.com/QuantumNous/new-api/common"

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

// Error stat grouping dimensions for the stats page view toggle.
const (
	ErrorStatGroupBySignature   = "signature"   // 明细错误 (one bucket per signature hash)
	ErrorStatGroupByShortName   = "short_name"  // 明细错误类型 (bucket by admin short name)
	ErrorStatGroupByAttribution = "attribution" // 错误归因 (bucket by admin attribution)
)

// ErrorSignatureStat is one aggregated signature row with its per-model and
// per-channel breakdowns. The signature and its hash live in the log's Other
// JSON (no dedicated columns), so aggregation parses Other in Go rather than
// grouping in SQL — keeping it schema-free across every supported log DB
// (including ClickHouse).
type ErrorSignatureStat struct {
	SignatureHash string
	Signature     string
	Count         int64
	FirstSeen     int64
	LastSeen      int64
	Models        map[string]int64
	Channels      map[int]int64
}

// maxErrorStatScanRows caps how many error rows one stats request scans, so a
// missing/huge time window cannot pull the whole error-log table into memory.
const maxErrorStatScanRows = 50000

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

type errorStatScanRow struct {
	CreatedAt int64  `gorm:"column:created_at"`
	ModelName string `gorm:"column:model_name"`
	ChannelId int    `gorm:"column:channel_id"`
	Other     string `gorm:"column:other"`
}

type errorSignatureOther struct {
	ErrorSignature     string `json:"error_signature"`
	ErrorSignatureHash string `json:"error_signature_hash"`
}

// scanErrorStatRows pulls the capped error-log window for the filter, newest
// first, selecting only the columns the stats aggregations need.
func scanErrorStatRows(f ErrorStatFilter) ([]errorStatScanRow, error) {
	var rows []errorStatScanRow
	err := applyErrorStatFilter(LOG_DB.Table("logs"), f).
		Select("created_at, model_name, channel_id, other").
		Order("created_at desc").
		Limit(maxErrorStatScanRows).
		Scan(&rows).Error
	return rows, err
}

// GetErrorSignatureStats scans error logs in the filter window (capped), reads
// the signature + hash out of each row's Other JSON, and aggregates them by
// hash into the top-N signatures by occurrence count. Rows without a signature
// hash (older logs / non-error paths) are skipped.
func GetErrorSignatureStats(f ErrorStatFilter, limit int) ([]*ErrorSignatureStat, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := scanErrorStatRows(f)
	if err != nil {
		return nil, err
	}

	agg := make(map[string]*ErrorSignatureStat)
	for _, r := range rows {
		if r.Other == "" {
			continue
		}
		var o errorSignatureOther
		if common.Unmarshal([]byte(r.Other), &o) != nil || o.ErrorSignatureHash == "" {
			continue
		}
		s := agg[o.ErrorSignatureHash]
		if s == nil {
			s = &ErrorSignatureStat{
				SignatureHash: o.ErrorSignatureHash,
				Signature:     o.ErrorSignature,
				FirstSeen:     r.CreatedAt,
				LastSeen:      r.CreatedAt,
				Models:        make(map[string]int64),
				Channels:      make(map[int]int64),
			}
			agg[o.ErrorSignatureHash] = s
		}
		s.Count++
		if r.CreatedAt < s.FirstSeen {
			s.FirstSeen = r.CreatedAt
		}
		if r.CreatedAt > s.LastSeen {
			s.LastSeen = r.CreatedAt
		}
		if r.ModelName != "" {
			s.Models[r.ModelName]++
		}
		if r.ChannelId != 0 {
			s.Channels[r.ChannelId]++
		}
	}

	out := make([]*ErrorSignatureStat, 0, len(agg))
	for _, s := range agg {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ErrorGroupStat is one aggregated bucket for the stats page, independent of the
// grouping dimension. For signature grouping it is one signature (Signature /
// SignatureHash set); for short-name / attribution grouping it is one category
// (GroupLabel set, Signature empty). ShortName and Attribution carry the
// resolved values for the bucket where meaningful.
type ErrorGroupStat struct {
	GroupKey      string
	GroupLabel    string
	Signature     string
	SignatureHash string
	ShortName     string
	Attribution   string
	Note          string
	Count         int64
	FirstSeen     int64
	LastSeen      int64
	Models        map[string]int64
	Channels      map[int]int64
}

// GetErrorStatsGrouped scans the error-log window (capped) and aggregates it by
// the requested dimension: signature (one bucket per signature hash),
// short_name (明细错误类型), or attribution (错误归因). Short-name / attribution
// values are resolved per signature through the in-memory alias cache, so the
// bucketing reflects the current admin configuration. All scanned rows are
// bucketed before the top-N cut, so counts and shares are accurate over the
// full window rather than a client-side regroup of a top-N signature list.
func GetErrorStatsGrouped(f ErrorStatFilter, groupBy string, limit int) ([]*ErrorGroupStat, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if groupBy == ErrorStatGroupBySignature {
		stats, err := GetErrorSignatureStats(f, limit)
		if err != nil {
			return nil, err
		}
		out := make([]*ErrorGroupStat, 0, len(stats))
		for _, s := range stats {
			out = append(out, &ErrorGroupStat{
				GroupKey:      s.SignatureHash,
				GroupLabel:    s.Signature,
				Signature:     s.Signature,
				SignatureHash: s.SignatureHash,
				ShortName:     ResolveErrorShortName(s.SignatureHash),
				Attribution:   ResolveErrorAttribution(s.SignatureHash),
				Note:          ResolveErrorNote(s.SignatureHash),
				Count:         s.Count,
				FirstSeen:     s.FirstSeen,
				LastSeen:      s.LastSeen,
				Models:        s.Models,
				Channels:      s.Channels,
			})
		}
		return out, nil
	}

	rows, err := scanErrorStatRows(f)
	if err != nil {
		return nil, err
	}

	agg := make(map[string]*ErrorGroupStat)
	for _, r := range rows {
		if r.Other == "" {
			continue
		}
		var o errorSignatureOther
		if common.Unmarshal([]byte(r.Other), &o) != nil || o.ErrorSignatureHash == "" {
			continue
		}
		key := ResolveErrorShortName(o.ErrorSignatureHash)
		if groupBy == ErrorStatGroupByAttribution {
			key = ResolveErrorAttribution(o.ErrorSignatureHash)
		}
		s := agg[key]
		if s == nil {
			s = &ErrorGroupStat{
				GroupKey:   key,
				GroupLabel: key,
				FirstSeen:  r.CreatedAt,
				LastSeen:   r.CreatedAt,
				Models:     make(map[string]int64),
				Channels:   make(map[int]int64),
			}
			if groupBy == ErrorStatGroupByAttribution {
				s.Attribution = key
			} else {
				s.ShortName = key
			}
			agg[key] = s
		}
		s.Count++
		if r.CreatedAt < s.FirstSeen {
			s.FirstSeen = r.CreatedAt
		}
		if r.CreatedAt > s.LastSeen {
			s.LastSeen = r.CreatedAt
		}
		if r.ModelName != "" {
			s.Models[r.ModelName]++
		}
		if r.ChannelId != 0 {
			s.Channels[r.ChannelId]++
		}
	}

	out := make([]*ErrorGroupStat, 0, len(agg))
	for _, s := range agg {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
