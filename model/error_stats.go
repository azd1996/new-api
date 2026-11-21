package model

import (
	"sort"
	"strings"

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
	// CustomerId / CustomerName narrow by the downstream customer recorded in the
	// log's Other JSON. A value of customerNoneFilter ("__none__") selects error
	// logs that carry no decoded customer, mirroring the usage-log filters.
	CustomerId   string
	CustomerName string
	// ShortName / Attribution narrow by the persisted classification (new logs)
	// or the alias cache (legacy logs). They are applied in Go after the SQL scan.
	ShortName   string
	Attribution string
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
	ShortName     string
	Attribution   string
	Note          string
	Count         int64
	FirstSeen     int64
	LastSeen      int64
	Models        map[string]int64
	Channels      map[int]int64
	// Customers aggregates occurrences by downstream customer id (from the
	// log's Other JSON). Rows without a customer id are skipped.
	Customers map[string]int64
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
		// status_code lives in a dedicated indexed column for rows written since
		// the column was added; older rows carry it only in the Other JSON blob.
		// Match either so historical error logs stay searchable.
		condition, args := statusCodeOtherCondition("other", f.StatusCode)
		tx = tx.Where("(status_code = ? OR "+condition+")", append([]any{f.StatusCode}, args...)...)
	}
	if f.CustomerId == customerNoneFilter || f.CustomerName == customerNoneFilter {
		cond, pat := otherFieldAbsentCondition("other", "customer_id")
		tx = tx.Where(cond, pat)
	} else {
		if f.CustomerId != "" {
			cond, pat := otherFieldLikeCondition("other", "customer_id", f.CustomerId)
			tx = tx.Where(cond, pat)
		}
		if f.CustomerName != "" {
			cond, pat := otherFieldLikeCondition("other", "customer_name", f.CustomerName)
			tx = tx.Where(cond, pat)
		}
	}
	return tx
}

// matchesErrorCategoryFilter reports whether a row passes the short-name
// (明细错误类型) and attribution (错误归因) filters.
func matchesErrorCategoryFilter(f ErrorStatFilter, o errorSignatureOther, hash string) bool {
	if f.ShortName != "" && o.detailedErrorType(hash) != f.ShortName {
		return false
	}
	if f.Attribution != "" && o.attribution(hash) != f.Attribution {
		return false
	}
	return true
}

type errorStatScanRow struct {
	CreatedAt int64  `gorm:"column:created_at"`
	ModelName string `gorm:"column:model_name"`
	ChannelId int    `gorm:"column:channel_id"`
	Other     string `gorm:"column:other"`
}

// errorSignatureOther reads the signature, its hash, and the decoded customer
// out of an error log's Other JSON. The strongly-typed LogOther stores these
// admin-tier fields under its "admin_info" section (see RecordErrorLog /
// LogOther.SetAdmin), so they are read from there first. The flat top-level
// fields are a fallback for error logs written before the LogOther
// restructure, which stored the same keys at the root.
type errorSignatureOther struct {
	AdminInfo struct {
		ErrorSignature     string `json:"error_signature"`
		ErrorSignatureHash string `json:"error_signature_hash"`
		ErrorDetailedType  string `json:"error_detailed_type"`
		ErrorAttribution   string `json:"error_attribution"`
		ErrorNote          string `json:"error_note"`
		CustomerId         string `json:"customer_id"`
	} `json:"admin_info"`
	LegacyErrorSignature     string `json:"error_signature"`
	LegacyErrorSignatureHash string `json:"error_signature_hash"`
	LegacyCustomerId         string `json:"customer_id"`
}

func (o errorSignatureOther) signature() string {
	if o.AdminInfo.ErrorSignature != "" {
		return o.AdminInfo.ErrorSignature
	}
	return o.LegacyErrorSignature
}

func (o errorSignatureOther) signatureHash() string {
	if o.AdminInfo.ErrorSignatureHash != "" {
		return o.AdminInfo.ErrorSignatureHash
	}
	return o.LegacyErrorSignatureHash
}

func (o errorSignatureOther) customerId() string {
	if o.AdminInfo.CustomerId != "" {
		return o.AdminInfo.CustomerId
	}
	return o.LegacyCustomerId
}

func (o errorSignatureOther) detailedErrorType(hash string) string {
	if o.AdminInfo.ErrorDetailedType != "" {
		return o.AdminInfo.ErrorDetailedType
	}
	return ResolveErrorShortName(hash)
}

func (o errorSignatureOther) attribution(hash string) string {
	if o.AdminInfo.ErrorAttribution != "" {
		return o.AdminInfo.ErrorAttribution
	}
	return ResolveErrorAttribution(hash)
}

func (o errorSignatureOther) note(hash string) string {
	if o.AdminInfo.ErrorNote != "" {
		return o.AdminInfo.ErrorNote
	}
	return ResolveErrorNote(hash)
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

// GetErrorSignatureExamples returns up to limit raw (un-normalized) error
// contents recorded for the given signature hash, newest first. AI rule
// generation uses them to derive a regex that matches what production actually
// logs, rather than the already-normalized signature.
func GetErrorSignatureExamples(signatureHash string, limit int) ([]string, error) {
	if signatureHash == "" || limit <= 0 {
		return nil, nil
	}
	var rows []struct {
		Content string `gorm:"column:content"`
	}
	err := LOG_DB.Table("logs").
		Select("content").
		Where("type = ? AND error_signature_hash = ?", LogTypeError, signatureHash).
		Order("created_at desc").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	contents := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Content) != "" {
			contents = append(contents, row.Content)
		}
	}
	return contents, nil
}

// CountErrorLogs returns the total number of error logs matching the filter.
// It is intentionally separate from the top-N aggregation so callers can
// calculate shares against the complete window.
func CountErrorLogs(f ErrorStatFilter) (int64, error) {
	var count int64
	err := applyErrorStatFilter(LOG_DB.Model(&Log{}), f).Count(&count).Error
	return count, err
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
		if common.Unmarshal([]byte(r.Other), &o) != nil || o.signatureHash() == "" {
			continue
		}
		hash := o.signatureHash()
		s := agg[hash]
		if s == nil {
			s = &ErrorSignatureStat{
				SignatureHash: hash,
				Signature:     o.signature(),
				ShortName:     o.detailedErrorType(hash),
				Attribution:   o.attribution(hash),
				Note:          o.note(hash),
				FirstSeen:     r.CreatedAt,
				LastSeen:      r.CreatedAt,
				Models:        make(map[string]int64),
				Channels:      make(map[int]int64),
				Customers:     make(map[string]int64),
			}
			agg[hash] = s
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
		if cid := o.customerId(); cid != "" {
			s.Customers[cid]++
		}
	}

	out := make([]*ErrorSignatureStat, 0, len(agg))
	for _, s := range agg {
		if f.ShortName != "" && s.ShortName != f.ShortName {
			continue
		}
		if f.Attribution != "" && s.Attribution != f.Attribution {
			continue
		}
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
	// Customers aggregates occurrences by downstream customer id for the bucket.
	Customers map[string]int64
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
				ShortName:     s.ShortName,
				Attribution:   s.Attribution,
				Note:          s.Note,
				Count:         s.Count,
				FirstSeen:     s.FirstSeen,
				LastSeen:      s.LastSeen,
				Models:        s.Models,
				Channels:      s.Channels,
				Customers:     s.Customers,
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
		if common.Unmarshal([]byte(r.Other), &o) != nil || o.signatureHash() == "" {
			continue
		}
		hash := o.signatureHash()
		if !matchesErrorCategoryFilter(f, o, hash) {
			continue
		}
		key := o.detailedErrorType(hash)
		if groupBy == ErrorStatGroupByAttribution {
			key = o.attribution(hash)
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
				Customers:  make(map[string]int64),
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
		if cid := o.customerId(); cid != "" {
			s.Customers[cid]++
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
