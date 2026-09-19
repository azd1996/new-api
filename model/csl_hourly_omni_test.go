package model

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/third_party/csl-logshipper/reader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// omniTestSecretKey is a throwaway 32-byte AES key, base64-encoded.
const omniTestSecretKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// startOmniStub points the CSL hourly reader at a fake OmniDataSearch service
// and returns the requests it received. handler is given each decoded request
// and returns the raw response body to send back.
func startOmniStub(t *testing.T, pageSize int, handler func(req reader.Request) string) *[]reader.Request {
	t.Helper()
	got := make([]reader.Request, 0, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req reader.Request
		require.NoError(t, json.Unmarshal(body, &req))
		got = append(got, req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, handler(req))
	}))
	t.Cleanup(srv.Close)

	prev := []any{
		common.CslHourlyReaderEnabled, common.CslHourlyReaderBaseURL,
		common.CslHourlyReaderUsername, common.CslHourlyReaderSecretKey,
		common.CslHourlyReaderMetric, common.CslHourlyReaderInstanceName,
		common.CslHourlyReaderPageSize, common.CslHourlyReaderMaxRows,
		common.CslHourlyReaderTimeoutSecs,
	}
	t.Cleanup(func() {
		common.CslHourlyReaderEnabled = prev[0].(bool)
		common.CslHourlyReaderBaseURL = prev[1].(string)
		common.CslHourlyReaderUsername = prev[2].(string)
		common.CslHourlyReaderSecretKey = prev[3].(string)
		common.CslHourlyReaderMetric = prev[4].(string)
		common.CslHourlyReaderInstanceName = prev[5].(string)
		common.CslHourlyReaderPageSize = prev[6].(int)
		common.CslHourlyReaderMaxRows = prev[7].(int)
		common.CslHourlyReaderTimeoutSecs = prev[8].(int)
	})

	common.CslHourlyReaderEnabled = true
	common.CslHourlyReaderBaseURL = srv.URL
	common.CslHourlyReaderUsername = "test_user"
	common.CslHourlyReaderSecretKey = omniTestSecretKey
	common.CslHourlyReaderMetric = "xcdn_csl_sidecar_logs"
	common.CslHourlyReaderInstanceName = "diezhi7"
	common.CslHourlyReaderPageSize = pageSize
	common.CslHourlyReaderMaxRows = 1000
	common.CslHourlyReaderTimeoutSecs = 5
	return &got
}

// omniRow renders one dataItems entry with the field names and JSON types the
// cluster table uses.
func omniRow(startTime int64, username, statType string, settlementUSD float64, ts int64) string {
	return fmt.Sprintf(`{"start_time":%d,"end_time":%d,"user_id":1,"token_id":0,"channel_id":2,
"token_name":"tok","username":%q,"group_name":"diezhi","model_name":"gpt-5.5","stat_type":%q,
"cache_ttl":"","pricing_tier":"","token_bucket":"-","call_count":6,"tokens":300,
"unit_price_usd_per_million":1,"original_price_usd":%v,"settlement_price_usd":%v,
"usd_exchange_rate":7.3,"unit_price_cny_per_million":7.3,"original_price_cny":0.1,
"settlement_price_cny":0.1,"channel_discount":1,"ts":%d}`,
		startTime, startTime+3600, username, statType, settlementUSD, settlementUSD, ts)
}

func omniResp(total int, rows ...string) string {
	return fmt.Sprintf(`{"code":200,"message":"success","data":[{"metric":"xcdn_csl_sidecar_logs",
"dataItems":[%s],"otherValue":{"totalRecords":%d,"pageSize":8,"pageNum":1}}]}`,
		strings.Join(rows, ","), total)
}

// TestGetCslHourlyFromOmni_RequestShape pins down the query contract verified
// against the sandbox service: the upstream time window filters ts (write
// time), so business time must travel as an explicit hour list on start_time,
// the instance filter is mandatory, optional filters are equality, and paging
// starts at 1 because pageNum is 1-based upstream.
func TestGetCslHourlyFromOmni_RequestShape(t *testing.T) {
	got := startOmniStub(t, 10, func(reader.Request) string { return omniResp(0) })

	const startHour = int64(1788746400)
	before := time.Now().Unix()
	_, err := GetCslHourly(CslHourlyQuery{
		StartTimestamp: startHour,
		EndTimestamp:   startHour + 2*3600,
		GroupName:      "diezhi",
		ModelName:      "gpt-5.5",
	})
	require.NoError(t, err)
	require.Len(t, *got, 1)
	req := (*got)[0]

	assert.Equal(t, []string{"xcdn_csl_sidecar_logs"}, req.Metrics)
	assert.Equal(t, "SQL_TYPE", req.Format)
	assert.Equal(t, startHour, req.StartTime)
	assert.GreaterOrEqual(t, req.EndTime, before, "ts upper bound must cover rows written up to now")
	require.NotNil(t, req.LimitParams)
	assert.Equal(t, 1, req.LimitParams.PageNum)
	assert.Equal(t, 10, req.LimitParams.PageSize)
	assert.Equal(t, []reader.SortParam{
		{OrderField: "start_time", OrderType: "asc"},
		{OrderField: "ts", OrderType: "asc"},
	}, req.SortParams)
	assert.Equal(t, []reader.FilterParam{
		{Key: "x_instance_name", Value: []string{"diezhi7"}, Operation: "equal"},
		{Key: "start_time", Value: []string{"1788746400", "1788750000", "1788753600"}, Operation: "equal"},
		{Key: "group_name", Value: []string{"diezhi"}, Operation: "equal"},
		{Key: "model_name", Value: []string{"gpt-5.5"}, Operation: "equal"},
	}, req.FilterParams)
	assert.Len(t, req.SelectParams, len(cslHourlyOmniSelect))
}

// TestGetCslHourlyFromOmni_RerunKeepsNewestTs is the accounting invariant that
// distinguishes the cluster table from the local one: re-aggregating an hour
// appends rows with the same business key and a newer ts, and only the newest
// may be counted. Summing both would double the bill.
func TestGetCslHourlyFromOmni_RerunKeepsNewestTs(t *testing.T) {
	const startHour = int64(1788746400)
	startOmniStub(t, 10, func(reader.Request) string {
		return omniResp(3,
			omniRow(startHour, "xcdn", "standard_input", 0.5, 1788750000),
			omniRow(startHour, "xcdn", "standard_input", 0.7, 1788760000),
			omniRow(startHour, "xcdn", "standard_output", 0.2, 1788750000),
		)
	})

	rows, err := GetCslHourly(CslHourlyQuery{StartTimestamp: startHour, EndTimestamp: startHour})
	require.NoError(t, err)
	require.Len(t, rows, 2, "same business key twice must collapse to the newest ts")

	total := 0.0
	for _, row := range rows {
		total += row.SettlementPriceUsd
	}
	assert.InDelta(t, 0.9, total, 1e-9, "expected the rerun value 0.7 plus 0.2, not 0.5+0.7+0.2")
}

// TestGetCslHourlyFromOmni_PagesFromOneAndOrders walks three pages and checks
// the page numbers, because offset is (pageNum-1)*pageSize upstream: a loop
// starting at 0 would read the first page twice.
func TestGetCslHourlyFromOmni_PagesFromOneAndOrders(t *testing.T) {
	const h0 = int64(1788746400)
	// stat_type is a real aggregation dimension, so each row below is a
	// distinct business key. username is NOT a dimension (user_id is), which is
	// why it stays constant here.
	pages := map[int][]string{
		1: {omniRow(h0, "xcdn", "standard_input", 1, 1), omniRow(h0, "xcdn", "standard_output", 1, 1)},
		2: {omniRow(h0+3600, "xcdn", "standard_input", 1, 1), omniRow(h0+3600, "xcdn", "cache_hit", 1, 1)},
		3: {omniRow(h0+7200, "xcdn", "flat_call", 1, 1)},
	}
	got := startOmniStub(t, 2, func(req reader.Request) string {
		return omniResp(5, pages[req.LimitParams.PageNum]...)
	})

	rows, err := GetCslHourly(CslHourlyQuery{StartTimestamp: h0, EndTimestamp: h0 + 7200})
	require.NoError(t, err)
	require.Len(t, rows, 5)

	seen := make([]int, 0, 3)
	for _, req := range *got {
		seen = append(seen, req.LimitParams.PageNum)
	}
	assert.Equal(t, []int{1, 2, 3}, seen)

	// Same ordering contract as the LOG_DB path: start_time DESC.
	assert.Equal(t, h0+7200, rows[0].StartTime)
	assert.Equal(t, h0, rows[4].StartTime)
}

// An empty window is reported as the requested metric with empty dataItems, so
// a missing metric means the query was not understood and must not be reported
// as "no data".
func TestGetCslHourlyFromOmni_MissingMetricIsAnError(t *testing.T) {
	startOmniStub(t, 10, func(reader.Request) string {
		return `{"code":200,"message":"success","data":[]}`
	})
	_, err := GetCslHourly(CslHourlyQuery{StartTimestamp: 1788746400, EndTimestamp: 1788746400})
	require.Error(t, err)
}

// The service reports business failures as HTTP 200 with code 400; the upstream
// explanation must reach the caller instead of an empty result.
func TestGetCslHourlyFromOmni_BusinessErrorFails(t *testing.T) {
	startOmniStub(t, 10, func(reader.Request) string {
		return `{"code":400,"message":"Metric不存在: nope","data":null}`
	})
	_, err := GetCslHourly(CslHourlyQuery{StartTimestamp: 1788746400, EndTimestamp: 1788746400})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Metric不存在")
}

func TestCslHourlyStartTimes(t *testing.T) {
	hours, err := cslHourlyStartTimes(1788746400+59, 1788746400+2*3600)
	require.NoError(t, err)
	assert.Equal(t, []string{"1788746400", "1788750000", "1788753600"}, hours,
		"both bounds must be floored to the hour, inclusive of the end hour")

	_, err = cslHourlyStartTimes(0, 3600)
	require.Error(t, err)

	_, err = cslHourlyStartTimes(3600, 0)
	require.Error(t, err)

	// The generated SQL is an IN list, so an unbounded range must be rejected
	// here rather than turned into an unbounded query.
	_, err = cslHourlyStartTimes(0+3600, 3600+int64(cslHourlyMaxHours)*3600)
	require.Error(t, err)
}
