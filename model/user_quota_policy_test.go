package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedQuotaPolicyUser(t *testing.T, user *User) {
	t.Helper()
	require.NoError(t, DB.Create(user).Error)
}

func seedQuotaPolicy(t *testing.T, userId int, mode string, amount int, enabled bool) *UserQuotaPolicy {
	t.Helper()
	policy, err := SaveUserQuotaPolicy(userId, mode, amount, enabled, common.RoleRootUser)
	require.NoError(t, err)
	return policy
}

func getUserQuota(t *testing.T, userId int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	return user.Quota
}

func countQuotaGrantLogs(t *testing.T, userId int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, LOG_DB.Model(&Log{}).Where("user_id = ? AND type = ?", userId, LogTypeTopup).Count(&count).Error)
	return count
}

func TestGrantUserQuotaPolicySettlesOncePerCalendarMonth(t *testing.T) {
	truncateTables(t)
	seedQuotaPolicyUser(t, &User{Id: 9301, Username: "monthly", Password: "x", AffCode: "aff9301", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 500})
	seedQuotaPolicy(t, 9301, QuotaPolicyModeMonthlyRefresh, 1000, true)

	adjustment, err := GrantUserQuotaPolicy(9301, "2026-12", nil)
	require.NoError(t, err)
	assert.Equal(t, 500, adjustment.Before)
	assert.Equal(t, 1000, adjustment.After)
	assert.Equal(t, 1000, getUserQuota(t, 9301))
	assert.Equal(t, int64(1), countQuotaGrantLogs(t, 9301))

	// The same calendar month must never settle twice, whatever the balance is.
	_, err = GrantUserQuotaPolicy(9301, "2026-12", nil)
	require.ErrorIs(t, err, ErrUserQuotaPolicyAlreadyGranted)
	assert.Equal(t, 1000, getUserQuota(t, 9301))
	assert.Equal(t, int64(1), countQuotaGrantLogs(t, 9301))

	// The next month overwrites whatever is left of the previous grant.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 9301).Update("quota", 10).Error)
	adjustment, err = GrantUserQuotaPolicy(9301, "2027-01", nil)
	require.NoError(t, err)
	assert.Equal(t, 10, adjustment.Before)
	assert.Equal(t, 1000, adjustment.After)
	assert.Equal(t, 1000, getUserQuota(t, 9301))

	policy, err := GetUserQuotaPolicy(9301)
	require.NoError(t, err)
	assert.Equal(t, "2027-01", policy.LastGrantedPeriod)
}

func TestSaveUserQuotaPolicyKeepsSettledPeriod(t *testing.T) {
	truncateTables(t)
	seedQuotaPolicyUser(t, &User{Id: 9302, Username: "edited", Password: "x", AffCode: "aff9302", Role: common.RoleCommonUser, Status: common.UserStatusEnabled})
	policy := seedQuotaPolicy(t, 9302, QuotaPolicyModeMonthlyRefresh, 1000, true)

	_, err := GrantUserQuotaPolicy(9302, "2026-12", nil)
	require.NoError(t, err)

	policy, err = SaveUserQuotaPolicy(9302, QuotaPolicyModeMonthlyRefresh, 2000, true, common.RoleRootUser)
	require.NoError(t, err)
	assert.Equal(t, "2026-12", policy.LastGrantedPeriod)

	// Editing the amount does not re-open the settled month.
	_, err = GrantUserQuotaPolicy(9302, "2026-12", nil)
	require.ErrorIs(t, err, ErrUserQuotaPolicyAlreadyGranted)
	assert.Equal(t, 1000, getUserQuota(t, 9302))
}

func TestGrantUserQuotaPolicyRejectsIneligiblePolicies(t *testing.T) {
	truncateTables(t)
	seedQuotaPolicyUser(t, &User{Id: 9303, Username: "fixed", Password: "x", AffCode: "aff9303", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 700})
	seedQuotaPolicyUser(t, &User{Id: 9304, Username: "disabled", Password: "x", AffCode: "aff9304", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, Quota: 700})
	seedQuotaPolicy(t, 9303, QuotaPolicyModeFixed, 1000, true)
	seedQuotaPolicy(t, 9304, QuotaPolicyModeMonthlyRefresh, 1000, true)

	_, err := GrantUserQuotaPolicy(9303, "2026-12", nil)
	require.ErrorIs(t, err, ErrUserQuotaPolicyInvalidMode)

	_, err = GrantUserQuotaPolicy(9304, "2026-12", nil)
	require.ErrorIs(t, err, ErrUserQuotaPolicyUserDisabled)

	assert.Equal(t, 700, getUserQuota(t, 9303))
	assert.Equal(t, 700, getUserQuota(t, 9304))
	assert.Zero(t, countQuotaGrantLogs(t, 9303))
	assert.Zero(t, countQuotaGrantLogs(t, 9304))

	_, err = GrantUserQuotaPolicy(9305, "2026-12", nil)
	require.ErrorIs(t, err, ErrUserQuotaPolicyNotFound)
}

func TestListDueMonthlyQuotaRefreshPoliciesOnlyReturnsUnsettledCurrentPeriod(t *testing.T) {
	truncateTables(t)
	period := CurrentQuotaPeriod()
	previousPeriod := "1999-01"

	seedQuotaPolicyUser(t, &User{Id: 9306, Username: "due", Password: "x", AffCode: "aff9306", Role: common.RoleCommonUser, Status: common.UserStatusEnabled})
	seedQuotaPolicyUser(t, &User{Id: 9307, Username: "settled", Password: "x", AffCode: "aff9307", Role: common.RoleCommonUser, Status: common.UserStatusEnabled})
	seedQuotaPolicyUser(t, &User{Id: 9308, Username: "paused", Password: "x", AffCode: "aff9308", Role: common.RoleCommonUser, Status: common.UserStatusEnabled})
	seedQuotaPolicyUser(t, &User{Id: 9309, Username: "fixed", Password: "x", AffCode: "aff9309", Role: common.RoleCommonUser, Status: common.UserStatusEnabled})

	seedQuotaPolicy(t, 9306, QuotaPolicyModeMonthlyRefresh, 1000, true)
	seedQuotaPolicy(t, 9307, QuotaPolicyModeMonthlyRefresh, 1000, true)
	_, err := GrantUserQuotaPolicy(9307, period, nil)
	require.NoError(t, err)
	seedQuotaPolicy(t, 9308, QuotaPolicyModeMonthlyRefresh, 1000, false)
	seedQuotaPolicy(t, 9309, QuotaPolicyModeFixed, 1000, true)
	require.NoError(t, DB.Model(&UserQuotaPolicy{}).Where("user_id = ?", 9306).
		Update("last_granted_period", previousPeriod).Error)

	policies, err := ListDueMonthlyQuotaRefreshPolicies(100)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, 9306, policies[0].UserId)

	// The settled user keeps the period that excludes it from the due list.
	settled, err := GetUserQuotaPolicy(9307)
	require.NoError(t, err)
	assert.Equal(t, period, settled.LastGrantedPeriod)
}

func TestSaveUserQuotaPolicyValidatesInput(t *testing.T) {
	truncateTables(t)

	_, err := SaveUserQuotaPolicy(9310, "weekly", 1000, true, common.RoleRootUser)
	require.ErrorIs(t, err, ErrUserQuotaPolicyInvalidMode)

	_, err = SaveUserQuotaPolicy(9310, QuotaPolicyModeMonthlyRefresh, 0, true, common.RoleRootUser)
	require.ErrorIs(t, err, ErrInvalidUserQuotaAdjustment)

	_, err = SaveUserQuotaPolicy(9310, QuotaPolicyModeMonthlyRefresh, common.MaxWalletQuota+1, true, common.RoleRootUser)
	require.ErrorIs(t, err, ErrInvalidUserQuotaAdjustment)
}
