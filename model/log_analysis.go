package model

import (
	"fmt"
	"math"
	"sort"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// logAnalysisScanLimit caps how many consume-log rows a single analysis request
// will stream and hold in memory. Percentile math requires retaining each row's
// latency/FRT sample, so an unbounded filter (for example a very wide time
// range) could otherwise exhaust memory. When the cap is hit the result is
// marked Truncated so the UI can warn that the stats cover a partial sample.
const logAnalysisScanLimit = 500000

// logAnalysisBucketLabels are the prompt-token size buckets shown as the first
// column of the latency/FRT tables. Keep in sync with logAnalysisBucketIndex.
var logAnalysisBucketLabels = []string{"0-1K", "1-5K", "5-20K", "20-50K", "50-100K", "100K+"}

const logAnalysisBucketCount = 6

// logAnalysisBucketIndex maps a prompt-token count to its bucket row index.
// Boundary values fall into the upper bucket (e.g. exactly 1000 -> "1-5K").
func logAnalysisBucketIndex(promptTokens int) int {
	switch {
	case promptTokens < 1000:
		return 0
	case promptTokens < 5000:
		return 1
	case promptTokens < 20000:
		return 2
	case promptTokens < 50000:
		return 3
	case promptTokens < 100000:
		return 4
	default:
		return 5
	}
}

// LogAnalysisChannel identifies a channel column in the analysis tables.
type LogAnalysisChannel struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// LogLatencyCell is one latency/FRT table cell: aggregated latency and
// first-response-time (both in milliseconds) plus the sample count.
type LogLatencyCell struct {
	Lat float64 `json:"lat"`
	Frt float64 `json:"frt"`
	Cnt int     `json:"cnt"`
}

// LogLatencyBucketRow is a prompt-size row: the summary cell plus one cell per
// channel, aligned with LogAnalysisResult.Channels order.
type LogLatencyBucketRow struct {
	Bucket   string           `json:"bucket"`
	Total    LogLatencyCell   `json:"total"`
	Channels []LogLatencyCell `json:"channels"`
}

// LogLatencyTable is one statistic (mean / p50 / p90 / p99) across all buckets.
type LogLatencyTable struct {
	Buckets []LogLatencyBucketRow `json:"buckets"`
}

// LogCacheCell is one cache-hit cell. The hit rate is cache_read over total
// input tokens (for Claude, logged prompt_tokens excludes cache read/creation,
// so total input = prompt + cache_read + cache_creation; for OpenAI the cached
// tokens are already part of prompt_tokens). GrossRate uses all requests'
// input; EligibleRate excludes only the structurally-too-small requests (cold
// writes are still counted — from the customer's view, frequent upstream key
// rotation that forces cold writes is a real cache-hit loss). The four counts
// classify every request so the rates have context.
type LogCacheCell struct {
	GrossRate     float64 `json:"gross_rate"`
	EligibleRate  float64 `json:"eligible_rate"`
	CacheRead     int64   `json:"cache_read"`
	InputAll      int64   `json:"input_all"`
	InputEligible int64   `json:"input_eligible"`
	Hit           int     `json:"hit"`
	ColdWrite     int     `json:"cold_write"`
	TooSmall      int     `json:"too_small"`
	OtherMiss     int     `json:"other_miss"`
}

// LogCacheTable is the cache-hit breakdown: summary plus per-channel cells.
type LogCacheTable struct {
	Total    LogCacheCell   `json:"total"`
	Channels []LogCacheCell `json:"channels"`
}

// LogSuccessCell is one success-rate cell. Total is an approximate unique
// request count = consume + error - retry-override (error logs whose
// retry-override rule fired, i.e. intermediate retried attempts). Success is
// the consume count. Rate = success / total.
type LogSuccessCell struct {
	Total         int64   `json:"total"`
	Success       int64   `json:"success"`
	Error         int64   `json:"error"`
	RetryOverride int64   `json:"retry_override"`
	Rate          float64 `json:"rate"`
}

// LogSuccessTable is the success-rate breakdown: summary plus per-channel cells.
type LogSuccessTable struct {
	Total    LogSuccessCell   `json:"total"`
	Channels []LogSuccessCell `json:"channels"`
}

// LogAnalysisResult is the full payload for the log-analysis popup: the sorted
// channel columns, the prompt-size bucket labels, four latency/FRT tables (one
// per statistic) and the cache-hit table.
type LogAnalysisResult struct {
	Channels []LogAnalysisChannel `json:"channels"`
	Buckets  []string             `json:"bucket_labels"`
	Latency  struct {
		Mean LogLatencyTable `json:"mean"`
		P50  LogLatencyTable `json:"p50"`
		P90  LogLatencyTable `json:"p90"`
		P99  LogLatencyTable `json:"p99"`
	} `json:"latency"`
	Cache            LogCacheTable           `json:"cache"`
	Success          LogSuccessTable         `json:"success"`
	Thresholds       []CacheMinTokenRuleView `json:"thresholds"`
	ThresholdDefault int                     `json:"threshold_default"`
	Scanned          int                     `json:"scanned"`
	Truncated        bool                    `json:"truncated"`
}

// logAnalysisSamples accumulates the per-(channel,bucket) latency/FRT samples
// used to compute the latency statistics.
type logAnalysisSamples struct {
	lat []float64
	frt []float64
}

// cacheChannelAgg accumulates one channel's cache classification. Counts are
// mutually exclusive per request (hit > cold-write > too-small > other-miss).
// inputAll / inputWarm sum total input tokens (see LogCacheCell) rather than the
// raw prompt column, so the hit rate denominator is comparable to cache_read.
type cacheChannelAgg struct {
	inputAll      int64
	inputEligible int64 // total input tokens excluding only too-small requests (cold writes still counted)
	cacheRead     int64
	hit           int
	coldWrite     int
	tooSmall      int
	otherMiss     int
}

// logAnalysisScanRow holds only the columns the analysis needs from each log.
type logAnalysisScanRow struct {
	ChannelId    int    `gorm:"column:channel_id"`
	ModelName    string `gorm:"column:model_name"`
	PromptTokens int    `gorm:"column:prompt_tokens"`
	Other        string `gorm:"column:other"`
}

// logAnalysisOther pulls the latency/FRT/cache fields out of the log Other JSON
// (written by service.GenerateTextOtherInfo / GenerateClaudeOtherInfo: frt/lat
// in ms, cache_tokens = cache-read tokens, cache_creation_tokens = cache-write
// tokens, Claude only).
type logAnalysisOther struct {
	Frt                 float64 `json:"frt"`
	Lat                 float64 `json:"lat"`
	CacheTokens         float64 `json:"cache_tokens"`
	CacheCreationTokens float64 `json:"cache_creation_tokens"`
	Claude              bool    `json:"claude"`
}

// logStatSummary is the set of statistics computed for one latency/FRT sample.
type logStatSummary struct {
	mean, p50, p90, p99 float64
	cnt                 int
}

func summarizeLogSamples(values []float64) logStatSummary {
	n := len(values)
	if n == 0 {
		return logStatSummary{}
	}
	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	return logStatSummary{
		mean: sum / float64(n),
		p50:  nearestRankPercentile(sorted, 0.50),
		p90:  nearestRankPercentile(sorted, 0.90),
		p99:  nearestRankPercentile(sorted, 0.99),
		cnt:  n,
	}
}

// nearestRankPercentile returns the nearest-rank percentile of an already
// ascending-sorted slice. p is in (0,1]; empty input yields 0.
func nearestRankPercentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := int(math.Ceil(p*float64(n))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= n {
		rank = n - 1
	}
	return sorted[rank]
}

func logCacheHitRate(cacheRead, prompt int64) float64 {
	if prompt <= 0 {
		return 0
	}
	return float64(cacheRead) / float64(prompt)
}

// classifyCacheRow folds one request into a channel's cache counters. The
// classification is priority-ordered and mutually exclusive:
//   - hit: the request read something from cache.
//   - cold write: no read but it wrote cache (first/priming call; Claude only —
//     OpenAI does not report cache-write tokens, so its cold calls fall through).
//   - too small: input below the model's minimum cacheable length, so a hit was
//     structurally impossible (threshold 0 disables this bucket).
//   - other miss: eligible by size but still missed (dynamic prefix, no
//     cache_control, TTL expired, ...).
//
// totalInput is cache_read + cache_creation + prompt for Claude (whose logged
// prompt_tokens excludes cache read/creation) and just prompt for everyone else
// (OpenAI already folds cached tokens into prompt_tokens). It is the hit-rate
// denominator so the rate stays in [0,1]. InputEligible (the eligible-rate
// denominator) sums every non-too-small request's input — cold writes included,
// since from the customer's view a forced cold write is a real cache miss.
func classifyCacheRow(agg *cacheChannelAgg, promptTokens int, cacheRead, cacheCreation, minTokens int64, isClaude bool) {
	totalInput := int64(promptTokens)
	if isClaude {
		totalInput += cacheRead + cacheCreation
	}
	agg.inputAll += totalInput
	agg.cacheRead += cacheRead
	switch {
	case cacheRead > 0:
		agg.hit++
		agg.inputEligible += totalInput
	case cacheCreation > 0:
		agg.coldWrite++
		agg.inputEligible += totalInput
	case minTokens > 0 && totalInput < minTokens:
		agg.tooSmall++
	default:
		agg.otherMiss++
		agg.inputEligible += totalInput
	}
}

func cacheCellFromAgg(agg *cacheChannelAgg) LogCacheCell {
	return LogCacheCell{
		GrossRate:     logCacheHitRate(agg.cacheRead, agg.inputAll),
		EligibleRate:  logCacheHitRate(agg.cacheRead, agg.inputEligible),
		CacheRead:     agg.cacheRead,
		InputAll:      agg.inputAll,
		InputEligible: agg.inputEligible,
		Hit:           agg.hit,
		ColdWrite:     agg.coldWrite,
		TooSmall:      agg.tooSmall,
		OtherMiss:     agg.otherMiss,
	}
}

// GetLogAnalysis streams the consume logs matching the same admin filter set as
// GetAllLogs and aggregates them into the prompt-size latency/FRT tables and the
// cache-hit table. It is admin-only because the per-channel breakdown relies on
// resolving channel names, which user log queries never expose.
func GetLogAnalysis(startTimestamp int64, endTimestamp int64, filters LogFilterOptions, requestId string, upstreamRequestId string, statusCode int, errorKeyword string, customerId string, customerName string, downstreamRequestId string, downstreamTraceTag string, errorSignatureHash string) (*LogAnalysisResult, error) {
	tx := LOG_DB.Where("logs.type = ?", LogTypeConsume)
	tx, err := applyAdminLogFilters(tx, LogTypeConsume, startTimestamp, endTimestamp, filters, requestId, upstreamRequestId, statusCode, errorKeyword, customerId, customerName, downstreamRequestId, downstreamTraceTag, errorSignatureHash)
	if err != nil {
		return nil, err
	}

	rows, err := tx.Model(&Log{}).Select("logs.channel_id, logs.model_name, logs.prompt_tokens, logs.other").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// perChannel[channelId][bucketIndex] -> latency/FRT samples.
	perChannel := make(map[int]*[logAnalysisBucketCount]logAnalysisSamples)
	// cacheAgg[channelId] -> cache classification counters.
	cacheAgg := make(map[int]*cacheChannelAgg)
	scanned := 0
	truncated := false
	for rows.Next() {
		if scanned >= logAnalysisScanLimit {
			truncated = true
			break
		}
		var r logAnalysisScanRow
		if err := LOG_DB.ScanRows(rows, &r); err != nil {
			return nil, err
		}
		scanned++

		buckets := perChannel[r.ChannelId]
		if buckets == nil {
			buckets = &[logAnalysisBucketCount]logAnalysisSamples{}
			perChannel[r.ChannelId] = buckets
		}
		s := &buckets[logAnalysisBucketIndex(r.PromptTokens)]
		var o logAnalysisOther
		if r.Other != "" {
			_ = common.UnmarshalJsonStr(r.Other, &o)
		}
		s.lat = append(s.lat, o.Lat)
		s.frt = append(s.frt, o.Frt)

		agg := cacheAgg[r.ChannelId]
		if agg == nil {
			agg = &cacheChannelAgg{}
			cacheAgg[r.ChannelId] = agg
		}
		classifyCacheRow(agg, r.PromptTokens, int64(o.CacheTokens), int64(o.CacheCreationTokens), int64(cacheMinTokensFor(r.ModelName)), o.Claude)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Success-rate counts come from cheap grouped COUNTs over consume + error
	// logs (not the capped stream), so they stay accurate regardless of the
	// 500k scan cap. They apply only the type-agnostic core filters (field /
	// time / request-id / customer / trace-tag), deliberately skipping the
	// error-drill filters (status_code / error_keyword / error_signature /
	// retry_triggered) which would otherwise zero out the consume side.
	consumeCounts, errorCounts, retryOverrideCounts, err := countConsumeErrorByChannel(startTimestamp, endTimestamp, filters, requestId, upstreamRequestId, customerId, customerName, downstreamRequestId, downstreamTraceTag)
	if err != nil {
		return nil, err
	}

	// Channel columns are the union of every channel that produced a consume
	// sample or any consume/error count, so an error-only channel still shows up
	// in the success table (its latency/cache cells render as dashes).
	idSet := make(map[int]bool)
	for id := range perChannel {
		idSet[id] = true
	}
	for id := range consumeCounts {
		idSet[id] = true
	}
	for id := range errorCounts {
		idSet[id] = true
	}
	channels := resolveLogAnalysisChannels(idSet)

	result := &LogAnalysisResult{
		Channels:         channels,
		Buckets:          logAnalysisBucketLabels,
		Thresholds:       CacheMinTokenRuleViews(),
		ThresholdDefault: cacheMinTokensDefault,
		Scanned:          scanned,
		Truncated:        truncated,
	}
	result.Latency.Mean = newLogLatencyTable(channels)
	result.Latency.P50 = newLogLatencyTable(channels)
	result.Latency.P90 = newLogLatencyTable(channels)
	result.Latency.P99 = newLogLatencyTable(channels)
	result.Cache.Channels = make([]LogCacheCell, len(channels))
	result.Success.Channels = make([]LogSuccessCell, len(channels))

	var totalAgg cacheChannelAgg
	var consumeSum, errorSum, retryOverrideSum int64
	for ci, ch := range channels {
		buckets := perChannel[ch.Id]
		if buckets == nil {
			// Error-only channel (no consume sample) — latency cells stay empty.
			buckets = &[logAnalysisBucketCount]logAnalysisSamples{}
		}
		for b := 0; b < logAnalysisBucketCount; b++ {
			s := buckets[b]
			latS := summarizeLogSamples(s.lat)
			frtS := summarizeLogSamples(s.frt)
			result.Latency.Mean.Buckets[b].Channels[ci] = LogLatencyCell{Lat: latS.mean, Frt: frtS.mean, Cnt: latS.cnt}
			result.Latency.P50.Buckets[b].Channels[ci] = LogLatencyCell{Lat: latS.p50, Frt: frtS.p50, Cnt: latS.cnt}
			result.Latency.P90.Buckets[b].Channels[ci] = LogLatencyCell{Lat: latS.p90, Frt: frtS.p90, Cnt: latS.cnt}
			result.Latency.P99.Buckets[b].Channels[ci] = LogLatencyCell{Lat: latS.p99, Frt: frtS.p99, Cnt: latS.cnt}
		}
		agg := cacheAgg[ch.Id]
		if agg == nil {
			agg = &cacheChannelAgg{}
		}
		result.Cache.Channels[ci] = cacheCellFromAgg(agg)
		totalAgg.inputAll += agg.inputAll
		totalAgg.inputEligible += agg.inputEligible
		totalAgg.cacheRead += agg.cacheRead
		totalAgg.hit += agg.hit
		totalAgg.coldWrite += agg.coldWrite
		totalAgg.tooSmall += agg.tooSmall
		totalAgg.otherMiss += agg.otherMiss

		consume := consumeCounts[ch.Id]
		errs := errorCounts[ch.Id]
		retryOverride := retryOverrideCounts[ch.Id]
		result.Success.Channels[ci] = successCellFrom(consume, errs, retryOverride)
		consumeSum += consume
		errorSum += errs
		retryOverrideSum += retryOverride
	}
	result.Cache.Total = cacheCellFromAgg(&totalAgg)
	result.Success.Total = successCellFrom(consumeSum, errorSum, retryOverrideSum)

	// The summary ("汇总") column pools every channel's samples per bucket, so it
	// needs the merged distribution rather than an average of per-channel stats.
	for b := 0; b < logAnalysisBucketCount; b++ {
		var totalLat, totalFrt []float64
		for _, ch := range channels {
			buckets := perChannel[ch.Id]
			if buckets == nil {
				continue
			}
			s := buckets[b]
			totalLat = append(totalLat, s.lat...)
			totalFrt = append(totalFrt, s.frt...)
		}
		latS := summarizeLogSamples(totalLat)
		frtS := summarizeLogSamples(totalFrt)
		result.Latency.Mean.Buckets[b].Total = LogLatencyCell{Lat: latS.mean, Frt: frtS.mean, Cnt: latS.cnt}
		result.Latency.P50.Buckets[b].Total = LogLatencyCell{Lat: latS.p50, Frt: frtS.p50, Cnt: latS.cnt}
		result.Latency.P90.Buckets[b].Total = LogLatencyCell{Lat: latS.p90, Frt: frtS.p90, Cnt: latS.cnt}
		result.Latency.P99.Buckets[b].Total = LogLatencyCell{Lat: latS.p99, Frt: frtS.p99, Cnt: latS.cnt}
	}

	return result, nil
}

// newLogLatencyTable allocates a latency table with one row per prompt bucket,
// each row pre-sized to hold a cell for every channel column.
func newLogLatencyTable(channels []LogAnalysisChannel) LogLatencyTable {
	table := LogLatencyTable{Buckets: make([]LogLatencyBucketRow, logAnalysisBucketCount)}
	for b := range table.Buckets {
		table.Buckets[b] = LogLatencyBucketRow{
			Bucket:   logAnalysisBucketLabels[b],
			Channels: make([]LogLatencyCell, len(channels)),
		}
	}
	return table
}

// resolveLogAnalysisChannels turns a channel-id set into sorted columns,
// resolving display names from the channel cache or a single bulk query (the
// same source GetAllLogs uses). Channels sort by name, then id as a tiebreak.
func resolveLogAnalysisChannels(idSet map[int]bool) []LogAnalysisChannel {
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}

	names := make(map[int]string, len(ids))
	if len(ids) > 0 {
		if common.MemoryCacheEnabled {
			for _, id := range ids {
				if ch, err := CacheGetChannel(id); err == nil {
					names[id] = ch.Name
				}
			}
		} else {
			var rows []struct {
				Id   int    `gorm:"column:id"`
				Name string `gorm:"column:name"`
			}
			if err := DB.Table("channels").Select("id, name").Where("id IN ?", ids).Find(&rows).Error; err == nil {
				for _, row := range rows {
					names[row.Id] = row.Name
				}
			}
		}
	}

	channels := make([]LogAnalysisChannel, 0, len(ids))
	for _, id := range ids {
		name := names[id]
		if name == "" {
			name = fmt.Sprintf("#%d", id)
		}
		channels = append(channels, LogAnalysisChannel{Id: id, Name: name})
	}
	sort.Slice(channels, func(i, j int) bool {
		if channels[i].Name == channels[j].Name {
			return channels[i].Id < channels[j].Id
		}
		return channels[i].Name < channels[j].Name
	})
	return channels
}

// successCellFrom builds a success cell from the raw per-channel counts.
// Total approximates unique requests as consume + error - retry-override (the
// retried intermediate error attempts), clamped at 0; Success is the consume
// count; Rate is success / total.
func successCellFrom(consume, errs, retryOverride int64) LogSuccessCell {
	total := consume + errs - retryOverride
	if total < 0 {
		total = 0
	}
	rate := 0.0
	if total > 0 {
		rate = float64(consume) / float64(total)
	}
	return LogSuccessCell{
		Total:         total,
		Success:       consume,
		Error:         errs,
		RetryOverride: retryOverride,
		Rate:          rate,
	}
}

// countConsumeErrorByChannel returns per-channel counts of consume logs, error
// logs, and retry-override-triggered error logs, for the success-rate table.
// It applies only the type-agnostic core filters (field / time / request-id /
// customer / trace-tag) so the consume side is not zeroed out by error-only
// drill filters.
func countConsumeErrorByChannel(startTimestamp, endTimestamp int64, filters LogFilterOptions, requestId, upstreamRequestId, customerId, customerName, downstreamRequestId, downstreamTraceTag string) (consume, errs, retryOverride map[int]int64, err error) {
	applyCore := func(logType int) (*gorm.DB, error) {
		tx := LOG_DB.Where("logs.type = ?", logType)
		var e error
		if tx, e = applyLogFilterOptions(tx, filters, "logs."); e != nil {
			return nil, e
		}
		if requestId != "" {
			tx = tx.Where("logs.request_id = ?", requestId)
		}
		if upstreamRequestId != "" {
			tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
		}
		if startTimestamp != 0 {
			tx = tx.Where("logs.created_at >= ?", startTimestamp)
		}
		if endTimestamp != 0 {
			tx = tx.Where("logs.created_at <= ?", endTimestamp)
		}
		return applyTraceTagFilters(tx, "logs.other", customerId, customerName, downstreamRequestId, downstreamTraceTag), nil
	}

	groupCount := func(tx *gorm.DB) (map[int]int64, error) {
		var rows []struct {
			ChannelId int   `gorm:"column:channel_id"`
			Cnt       int64 `gorm:"column:cnt"`
		}
		if e := tx.Model(&Log{}).Select("logs.channel_id as channel_id, count(*) as cnt").Group("logs.channel_id").Scan(&rows).Error; e != nil {
			return nil, e
		}
		m := make(map[int]int64, len(rows))
		for _, r := range rows {
			m[r.ChannelId] = r.Cnt
		}
		return m, nil
	}

	consumeTx, err := applyCore(LogTypeConsume)
	if err != nil {
		return nil, nil, nil, err
	}
	if consume, err = groupCount(consumeTx); err != nil {
		return nil, nil, nil, err
	}

	errorTx, err := applyCore(LogTypeError)
	if err != nil {
		return nil, nil, nil, err
	}
	if errs, err = groupCount(errorTx); err != nil {
		return nil, nil, nil, err
	}

	retryTx, err := applyCore(LogTypeError)
	if err != nil {
		return nil, nil, nil, err
	}
	if retryOverride, err = groupCount(retryTx.Where("logs.retry_triggered = ?", true)); err != nil {
		return nil, nil, nil, err
	}

	return consume, errs, retryOverride, nil
}
