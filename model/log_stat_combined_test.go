package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestSumLogStatWithAmountsMatchesSeparate pins the invariant the fast path
// relies on: with no admin-only filter and an all/consume type scope, the single
// combined query must return exactly what SumUsedQuota + SumLogStatAmounts return
// separately. Consumption quota/count count only consume rows; the reporting
// amounts sum the stat columns (non-zero only on consume rows).
func TestSumLogStatWithAmountsMatchesSeparate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))

	oldDB, oldGroup, oldType := LOG_DB, logGroupCol, common.LogDatabaseType()
	LOG_DB, logGroupCol = db, "`group`"
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB, logGroupCol = oldDB, oldGroup
		common.SetLogDatabaseType(oldType)
		require.NoError(t, sqlDB.Close())
	})

	// Old created_at keeps the rolling-60s RPM/TPM window empty and deterministic.
	require.NoError(t, db.Create([]Log{
		{Id: 1, Type: LogTypeConsume, ModelName: "gpt-4", CreatedAt: 1000, Quota: 100, PromptTokens: 10, CompletionTokens: 5, StatQuota: 200, CustomerStatQuota: 300, CustomerDiscount: 0.9},
		{Id: 2, Type: LogTypeConsume, ModelName: "claude-3", CreatedAt: 2000, Quota: 50, PromptTokens: 7, CompletionTokens: 3, StatQuota: 80},
		{Id: 3, Type: LogTypeError, ModelName: "gpt-4", CreatedAt: 3000, Quota: 0},
		{Id: 4, Type: LogTypeTopup, ModelName: "gpt-4", CreatedAt: 4000, Quota: 999},
	}).Error)

	for _, tc := range []struct {
		name    string
		filters LogFilterOptions
	}{
		{"no filter", LogFilterOptions{}},
		{"model filter", LogFilterOptions{ModelName: LogFieldFilter{Values: []string{"gpt-4"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantStat, err := SumUsedQuota(LogTypeUnknown, 0, 0, tc.filters)
			require.NoError(t, err)
			wantAmounts, err := SumLogStatAmounts(LogTypeUnknown, 0, 0, tc.filters, "", "", 0, "", "", "", "", "", "")
			require.NoError(t, err)

			gotStat, gotAmounts, err := SumLogStatWithAmounts(0, 0, tc.filters)
			require.NoError(t, err)

			assert.Equal(t, wantStat.Quota, gotStat.Quota)
			assert.Equal(t, wantStat.RequestCount, gotStat.RequestCount)
			assert.Equal(t, wantStat.Rpm, gotStat.Rpm)
			assert.Equal(t, wantStat.Tpm, gotStat.Tpm)
			assert.Equal(t, wantAmounts.ChannelStatQuota, gotAmounts.ChannelStatQuota)
			assert.Equal(t, wantAmounts.CustomerStatQuota, gotAmounts.CustomerStatQuota)
			assert.Equal(t, wantAmounts.ProfitQuota, gotAmounts.ProfitQuota)
		})
	}

	// Lock the concrete expected values for the unfiltered case so a future
	// refactor cannot silently shift the aggregation semantics.
	stat, amounts, err := SumLogStatWithAmounts(0, 0, LogFilterOptions{})
	require.NoError(t, err)
	assert.Equal(t, 150, stat.Quota)                       // consume only: 100 + 50
	assert.Equal(t, 2, stat.RequestCount)                  // consume rows only
	assert.Equal(t, int64(280), amounts.ChannelStatQuota)  // 200 + 80
	assert.Equal(t, int64(300), amounts.CustomerStatQuota) // row 1 only
	assert.Equal(t, int64(100), amounts.ProfitQuota)       // row 1: 300 - 200
}
