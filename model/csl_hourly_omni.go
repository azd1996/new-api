package model

// This file adds the OmniDataSearch (问渠) read path for the hourly billing
// summary. It is an ALTERNATIVE to the LOG_DB query in csl_hourly.go, never a
// supplement: the two are mutually exclusive, selected by
// common.CslHourlyReaderEnabled, and an upstream failure is returned to the
// caller rather than silently falling back to a local table that may be empty
// after the log-backend migration.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/third_party/csl-logshipper/reader"
)

// cslHourlyOmniSelect lists the columns projected for every query. It must stay
// in sync with cslHourlyOmniRow's json tags: a column named here but absent
// upstream fails the whole query with a ClickHouse error rather than returning
// wrong data.
var cslHourlyOmniSelect = []string{
	"start_time", "end_time", "user_id", "token_id", "channel_id",
	"token_name", "username", "group_name", "model_name", "stat_type",
	"cache_ttl", "pricing_tier", "token_bucket", "call_count", "tokens",
	"unit_price_usd_per_million", "original_price_usd", "settlement_price_usd",
	"usd_exchange_rate", "unit_price_cny_per_million", "original_price_cny",
	"settlement_price_cny", "channel_discount", "ts",
}

// cslHourlyOmniRow is CslHourly plus the cluster table's ts column, which is
// the row's WRITE time (not business time) and doubles as the rerun version:
// re-aggregating an hour emits the same business keys with a newer ts, and only
// the newest may be counted. The local csl_hourly table has no equivalent
// because csl-sidecar replaces each window there in a single transaction.
type cslHourlyOmniRow struct {
	CslHourly
	Ts int64 `json:"ts"`
}

// cslHourlyBusinessKey is the aggregation identity of one summary row: the 11
// grouping dimensions csl-sidecar aggregates by. x_instance_name is part of the
// documented key too, but it is pinned to a constant by this query's filter.
type cslHourlyBusinessKey struct {
	StartTime   int64
	EndTime     int64
	UserId      int
	TokenId     int
	ChannelId   int
	ModelName   string
	GroupName   string
	StatType    string
	CacheTtl    string
	PricingTier string
	TokenBucket string
}

// cslHourlyOmniTsSlackSeconds widens the upstream ts window past "now" so a row
// written while this query is in flight, or written by a host whose clock runs
// slightly ahead, is still inside the window.
const cslHourlyOmniTsSlackSeconds = 3600

func getCslHourlyFromOmni(params CslHourlyQuery) ([]*CslHourly, error) {
	client, err := reader.New(reader.Config{
		BaseURL:   common.CslHourlyReaderBaseURL,
		Username:  common.CslHourlyReaderUsername,
		SecretKey: common.CslHourlyReaderSecretKey,
		Timeout:   time.Duration(common.CslHourlyReaderTimeoutSecs) * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("csl hourly reader is misconfigured: %w", err)
	}
	if common.CslHourlyReaderInstanceName == "" {
		return nil, fmt.Errorf("csl hourly reader is misconfigured: instance name must not be empty")
	}
	metric := common.CslHourlyReaderMetric
	pageSize := common.CslHourlyReaderPageSize
	if pageSize <= 0 {
		pageSize = 5000
	}
	maxRows := common.CslHourlyReaderMaxRows
	if maxRows <= 0 {
		maxRows = 2000000
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(common.CslHourlyReaderTimeoutSecs)*time.Second*10)
	defer cancel()

	// newest keeps, per business key, the row with the largest ts.
	newest := make(map[cslHourlyBusinessKey]cslHourlyOmniRow)
	fetched := 0
	totalRecords := -1

	for pageNum := 1; ; pageNum++ {
		req, err := buildCslHourlyOmniRequest(params, metric, pageSize, pageNum)
		if err != nil {
			return nil, err
		}
		resp, err := client.Search(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("query csl hourly from OmniDataSearch (page %d): %w", pageNum, err)
		}
		// An empty result is still reported as the requested metric with empty
		// dataItems, so a missing metric means the query was not understood.
		if !reader.HasMetric(resp, metric) {
			return nil, fmt.Errorf("OmniDataSearch response carries no result for metric %s", metric)
		}
		items, err := reader.DecodeItems[cslHourlyOmniRow](resp, metric)
		if err != nil {
			return nil, err
		}
		if pageNum == 1 {
			meta, err := reader.DecodeOtherValue[struct {
				TotalRecords int `json:"totalRecords"`
			}](resp, metric)
			if err != nil {
				return nil, err
			}
			totalRecords = meta.TotalRecords
		}

		for _, item := range items {
			key := cslHourlyBusinessKey{
				StartTime: item.StartTime, EndTime: item.EndTime,
				UserId: item.UserId, TokenId: item.TokenId, ChannelId: item.ChannelId,
				ModelName: item.ModelName, GroupName: item.GroupName,
				StatType: item.StatType, CacheTtl: item.CacheTtl,
				PricingTier: item.PricingTier, TokenBucket: item.TokenBucket,
			}
			if prev, ok := newest[key]; ok && prev.Ts >= item.Ts {
				continue
			}
			newest[key] = item
		}

		fetched += len(items)
		if fetched > maxRows {
			return nil, fmt.Errorf("csl hourly query matched more than %d rows; narrow the time range", maxRows)
		}
		if len(items) < pageSize {
			break
		}
		if totalRecords >= 0 && fetched >= totalRecords {
			break
		}
	}

	rows := make([]*CslHourly, 0, len(newest))
	for _, item := range newest {
		row := item.CslHourly
		rows = append(rows, &row)
	}
	// Mirrors the LOG_DB path's Order("start_time DESC"); the extra keys only
	// make the order total, so repeating a query returns rows in one order.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.StartTime != b.StartTime {
			return a.StartTime > b.StartTime
		}
		if a.Username != b.Username {
			return a.Username < b.Username
		}
		if a.ModelName != b.ModelName {
			return a.ModelName < b.ModelName
		}
		if a.StatType != b.StatType {
			return a.StatType < b.StatType
		}
		return a.TokenBucket < b.TokenBucket
	})
	return rows, nil
}

// buildCslHourlyOmniRequest renders one page of the query.
//
// The upstream top-level startTime/endTime window filters on ts, the row's
// WRITE time — verified against the sandbox service, which echoes the SQL it
// generates. Business time therefore cannot be expressed through the window;
// it goes into filterParams as an explicit start_time list. filterParams has no
// range operator, but a multi-value `equal` becomes SQL `IN (...)`, and
// start_time is always hour-aligned, so a range of at most 7 days enumerates to
// at most 169 values.
//
// The window still has to be supplied, so it is set to
// [earliest requested hour, now + slack): a summary row is always written after
// the hour it aggregates, so its ts cannot precede the requested start, and a
// backfilled old hour has a recent ts, which this upper bound keeps.
func buildCslHourlyOmniRequest(params CslHourlyQuery, metric string, pageSize, pageNum int) (reader.Request, error) {
	hours, err := cslHourlyStartTimes(params.StartTimestamp, params.EndTimestamp)
	if err != nil {
		return reader.Request{}, err
	}

	filters := []reader.FilterParam{
		{Key: "x_instance_name", Value: []string{common.CslHourlyReaderInstanceName}, Operation: "equal"},
		{Key: "start_time", Value: hours, Operation: "equal"},
	}
	for _, optional := range []struct {
		key   string
		value string
	}{
		{"group_name", params.GroupName},
		{"username", params.Username},
		{"model_name", params.ModelName},
		{"token_name", params.TokenName},
	} {
		if optional.value != "" {
			filters = append(filters, reader.FilterParam{
				Key: optional.key, Value: []string{optional.value}, Operation: "equal",
			})
		}
	}

	selects := make([]reader.SelectParam, 0, len(cslHourlyOmniSelect))
	for _, attr := range cslHourlyOmniSelect {
		selects = append(selects, reader.SelectParam{TableAttribute: attr})
	}

	return reader.Request{
		Metrics:      []string{metric},
		StartTime:    params.StartTimestamp,
		EndTime:      time.Now().Unix() + cslHourlyOmniTsSlackSeconds,
		SelectParams: selects,
		FilterParams: filters,
		SortParams: []reader.SortParam{
			{OrderField: "start_time", OrderType: "asc"},
			{OrderField: "ts", OrderType: "asc"},
		},
		LimitParams: &reader.LimitParam{PageSize: pageSize, PageNum: pageNum},
		Format:      "SQL_TYPE",
	}, nil
}

// cslHourlyMaxHours bounds the enumerated start_time list. The controller
// already caps the span at 7 days; this guards the model layer independently,
// because an unbounded list would be turned into an unbounded SQL IN clause.
const cslHourlyMaxHours = 24*7 + 1

// cslHourlyStartTimes enumerates the hour-aligned start_time values covered by
// [start, end], as decimal strings for filterParams.
func cslHourlyStartTimes(start, end int64) ([]string, error) {
	if start <= 0 || end < start {
		return nil, fmt.Errorf("invalid csl hourly time range [%d, %d]", start, end)
	}
	first := start - start%3600
	last := end - end%3600
	count := (last-first)/3600 + 1
	if count > cslHourlyMaxHours {
		return nil, fmt.Errorf("csl hourly time range spans %d hours, at most %d are supported", count, cslHourlyMaxHours)
	}
	hours := make([]string, 0, count)
	for hour := first; hour <= last; hour += 3600 {
		hours = append(hours, strconv.FormatInt(hour, 10))
	}
	return hours, nil
}
