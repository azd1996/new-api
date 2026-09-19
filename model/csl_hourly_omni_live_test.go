package model

import (
	"os"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLiveCslHourlyFromOmni runs the real OmniDataSearch read path against a
// real service. Its main assertion is that the query is accepted: every column
// in cslHourlyOmniSelect must exist upstream, and a name that does not fails
// the whole query with a ClickHouse error rather than returning wrong data. Row
// contents are whatever that instance happened to aggregate, so only the
// invariants the API contract depends on are checked.
//
// Gated on environment variables rather than a build tag so a rename in the
// query builder breaks the build immediately instead of rotting; with
// credentials absent it simply skips.
//
//	CSL_HOURLY_READER_BASE_URL=http://10.164.22.41:8785 \
//	CSL_HOURLY_READER_USERNAME=test_user \
//	CSL_HOURLY_READER_SECRET_KEY=<base64 aes key> \
//	CSL_HOURLY_READER_INSTANCE_NAME=diezhi7 \
//	CSL_HOURLY_READER_START=1788746400 CSL_HOURLY_READER_END=1788832800 \
//	go test ./model/ -run TestLiveCslHourly -v -count=1
//
// The secret key is never logged.
func TestLiveCslHourlyFromOmni(t *testing.T) {
	baseURL := os.Getenv("CSL_HOURLY_READER_BASE_URL")
	username := os.Getenv("CSL_HOURLY_READER_USERNAME")
	secretKey := os.Getenv("CSL_HOURLY_READER_SECRET_KEY")
	instance := os.Getenv("CSL_HOURLY_READER_INSTANCE_NAME")
	start := liveCslHourlyEnvInt(t, "CSL_HOURLY_READER_START")
	end := liveCslHourlyEnvInt(t, "CSL_HOURLY_READER_END")
	if baseURL == "" || username == "" || secretKey == "" || instance == "" || start == 0 || end == 0 {
		t.Skip("live test skipped: set CSL_HOURLY_READER_BASE_URL/USERNAME/SECRET_KEY/INSTANCE_NAME/START/END")
	}

	prevEnabled, prevBaseURL := common.CslHourlyReaderEnabled, common.CslHourlyReaderBaseURL
	prevUser, prevKey := common.CslHourlyReaderUsername, common.CslHourlyReaderSecretKey
	prevMetric, prevInstance := common.CslHourlyReaderMetric, common.CslHourlyReaderInstanceName
	prevPage, prevMax, prevTimeout := common.CslHourlyReaderPageSize, common.CslHourlyReaderMaxRows, common.CslHourlyReaderTimeoutSecs
	t.Cleanup(func() {
		common.CslHourlyReaderEnabled, common.CslHourlyReaderBaseURL = prevEnabled, prevBaseURL
		common.CslHourlyReaderUsername, common.CslHourlyReaderSecretKey = prevUser, prevKey
		common.CslHourlyReaderMetric, common.CslHourlyReaderInstanceName = prevMetric, prevInstance
		common.CslHourlyReaderPageSize, common.CslHourlyReaderMaxRows, common.CslHourlyReaderTimeoutSecs = prevPage, prevMax, prevTimeout
	})

	common.CslHourlyReaderEnabled = true
	common.CslHourlyReaderBaseURL = baseURL
	common.CslHourlyReaderUsername = username
	common.CslHourlyReaderSecretKey = secretKey
	common.CslHourlyReaderInstanceName = instance
	common.CslHourlyReaderMetric = "xcdn_csl_sidecar_logs"
	if metric := os.Getenv("CSL_HOURLY_READER_METRIC"); metric != "" {
		common.CslHourlyReaderMetric = metric
	}
	common.CslHourlyReaderPageSize = 1000
	common.CslHourlyReaderMaxRows = 200000
	common.CslHourlyReaderTimeoutSecs = 30

	rows, err := GetCslHourly(CslHourlyQuery{StartTimestamp: start, EndTimestamp: end})
	require.NoError(t, err, "every projected column must exist upstream")
	t.Logf("metric=%s instance=%s range=[%d,%d] rows=%d",
		common.CslHourlyReaderMetric, instance, start, end, len(rows))

	seen := make(map[cslHourlyBusinessKey]struct{}, len(rows))
	var prevStart int64
	for i, row := range rows {
		if i < 3 {
			t.Logf("  row[%d] start=%d user=%d/%q model=%q stat=%s tokens=%d usd=%v",
				i, row.StartTime, row.UserId, row.Username, row.ModelName,
				row.StatType, row.Tokens, row.SettlementPriceUsd)
		}
		assert.GreaterOrEqual(t, row.StartTime, start-start%3600, "row outside requested range")
		assert.LessOrEqual(t, row.StartTime, end, "row outside requested range")
		assert.Equal(t, row.StartTime+3600, row.EndTime, "end_time must be start_time + one hour")
		if i > 0 {
			assert.LessOrEqual(t, row.StartTime, prevStart, "rows must be ordered start_time DESC")
		}
		prevStart = row.StartTime

		key := cslHourlyBusinessKey{
			StartTime: row.StartTime, EndTime: row.EndTime,
			UserId: row.UserId, TokenId: row.TokenId, ChannelId: row.ChannelId,
			ModelName: row.ModelName, GroupName: row.GroupName,
			StatType: row.StatType, CacheTtl: row.CacheTtl,
			PricingTier: row.PricingTier, TokenBucket: row.TokenBucket,
		}
		_, dup := seen[key]
		assert.False(t, dup, "a business key survived rerun deduplication twice: %+v", key)
		seen[key] = struct{}{}
	}
}

// liveCslHourlyEnvInt returns 0 when key is unset. A set-but-unparseable value
// fails the test rather than silently skipping, so a typo in the run command is
// not mistaken for "no credentials".
func liveCslHourlyEnvInt(t *testing.T, key string) int64 {
	t.Helper()
	raw := os.Getenv(key)
	if raw == "" {
		return 0
	}
	var value int64
	value, err := strconv.ParseInt(raw, 10, 64)
	require.NoErrorf(t, err, "%s=%q is not an integer", key, raw)
	return value
}
