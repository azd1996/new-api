package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateErrorAlertRulesJSON(t *testing.T) {
	rules := `[{"id":"customer-a","enabled":true,"customer_id":"cust-a","top_n":0,"alert_toid":[1001],"alert_url":"https://example.com/ruliu"}]`

	require.NoError(t, ValidateErrorAlertRulesJSON(rules))
}

func TestValidateErrorAlertRulesJSONRejectsEnabledRuleWithoutDestination(t *testing.T) {
	rules := `[{"id":"customer-a","enabled":true,"customer_id":"cust-a","alert_url":"https://example.com/ruliu"}]`

	require.Error(t, ValidateErrorAlertRulesJSON(rules))
}
