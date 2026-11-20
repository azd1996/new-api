package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBalanceAlertRulesJSON(t *testing.T) {
	rules := `[{"id":"r1","enabled":true,"group":"vip","threshold_amount":50,"alert_toid":[123],"alert_url":"https://example.com/hook"}]`
	require.NoError(t, ValidateBalanceAlertRulesJSON(rules))
}

func TestValidateBalanceAlertRulesJSONRejectsUnusableRules(t *testing.T) {
	cases := map[string]string{
		"not an array":       `{"id":"r1"}`,
		"missing id":         `[{"enabled":false}]`,
		"duplicate id":       `[{"id":"r1","enabled":false},{"id":"r1","enabled":false}]`,
		"no threshold":       `[{"id":"r1","enabled":true,"alert_toid":[1],"alert_url":"https://example.com"}]`,
		"no destination":     `[{"id":"r1","enabled":true,"threshold_amount":10,"alert_url":"https://example.com"}]`,
		"non http alert url": `[{"id":"r1","enabled":true,"threshold_amount":10,"alert_toid":[1],"alert_url":"ftp://example.com"}]`,
	}
	for name, rules := range cases {
		require.Error(t, ValidateBalanceAlertRulesJSON(rules), name)
	}
}

func TestNormalizeBalanceAlertRuleClampsMaxUsers(t *testing.T) {
	rule := BalanceAlertRule{MaxUsers: 0}
	normalizeBalanceAlertRule(&rule)
	require.Equal(t, DefaultBalanceAlertMaxUsers, rule.MaxUsers)

	rule = BalanceAlertRule{MaxUsers: MaxBalanceAlertMaxUsers + 1}
	normalizeBalanceAlertRule(&rule)
	require.Equal(t, MaxBalanceAlertMaxUsers, rule.MaxUsers)
}
