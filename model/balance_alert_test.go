package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedBalanceAlertUser(t *testing.T, user *User) {
	t.Helper()
	require.NoError(t, DB.Create(user).Error)
}

// The rule group keeps the scan independent of users created by other package
// tests, which are not guaranteed to be cleaned up before this one runs.
const balanceAlertTestGroup = "balance-alert-test"

func TestListUsersBelowBalanceSelectsOnlyUnsettledLowBalances(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	seedBalanceAlertUser(t, &User{Id: 9401, Username: "alerted", Password: "x", AffCode: "a9401", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 100, Group: balanceAlertTestGroup})
	seedBalanceAlertUser(t, &User{Id: 9402, Username: "lowest", Password: "x", AffCode: "a9402", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 50, Group: balanceAlertTestGroup})
	seedBalanceAlertUser(t, &User{Id: 9403, Username: "above", Password: "x", AffCode: "a9403", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 5000, Group: balanceAlertTestGroup})
	seedBalanceAlertUser(t, &User{Id: 9404, Username: "disabled", Password: "x", AffCode: "a9404", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, Quota: 10, Group: balanceAlertTestGroup})
	seedBalanceAlertUser(t, &User{Id: 9405, Username: "othergroup", Password: "x", AffCode: "a9405", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 10, Group: "default"})
	require.NoError(t, DB.Create(&BalanceAlertState{UserId: 9401, RuleId: "r1", LastAlertAt: now - 60}).Error)

	users, err := ListUsersBelowBalance("r1", 1000, balanceAlertTestGroup, now-3600, 10)
	require.NoError(t, err)

	// Only the enabled user below the threshold that was not alerted inside the
	// quiet window remains; the lowest balance would otherwise come first.
	require.Len(t, users, 1)
	assert.Equal(t, 9402, users[0].Id)
}

func TestMarkBalanceAlertedMovesTheWindowForward(t *testing.T) {
	truncateTables(t)

	require.NoError(t, MarkBalanceAlerted("r3", []int{9421, 9422}))
	require.NoError(t, MarkBalanceAlerted("r3", []int{9421}))

	var states []BalanceAlertState
	require.NoError(t, DB.Where("rule_id = ?", "r3").Order("user_id").Find(&states).Error)
	require.Len(t, states, 2)
	assert.Equal(t, 9421, states[0].UserId)
	assert.Positive(t, states[0].LastAlertAt)
}
