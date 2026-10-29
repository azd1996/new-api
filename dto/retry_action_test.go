package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryActionUnmarshalForms asserts phase4_retry_action accepts both the
// legacy bare-string form and the object form, and that a malformed value is
// tolerated (zero value) rather than failing the whole settings blob.
func TestRetryActionUnmarshalForms(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantAction string
		wantStatus int
		wantMsg    string
	}{
		{"string form", `"fallback_next_channel"`, "fallback_next_channel", 0, ""},
		{"object form", `{"action":"return_error","status_code":429,"status_msg":"rate limited"}`, "return_error", 429, "rate limited"},
		{"object action only", `{"action":"retry_same_channel"}`, "retry_same_channel", 0, ""},
		{"malformed tolerated", `12345`, "", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a RetryAction
			require.NoError(t, common.UnmarshalJsonStr(tc.raw, &a))
			assert.Equal(t, tc.wantAction, a.Action)
			assert.Equal(t, tc.wantStatus, a.StatusCode)
			assert.Equal(t, tc.wantMsg, a.StatusMsg)
		})
	}
}

// TestRetryActionMarshalRoundTrip asserts the bare-string form is preserved for
// action-only values (backward compatible) and the object form is emitted once a
// status code/message is present.
func TestRetryActionMarshalRoundTrip(t *testing.T) {
	stringOnly, err := common.Marshal(RetryAction{Action: "retry_same_channel"})
	require.NoError(t, err)
	assert.JSONEq(t, `"retry_same_channel"`, string(stringOnly))

	object, err := common.Marshal(RetryAction{Action: "return_error", StatusCode: 403, StatusMsg: "no"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"action":"return_error","status_code":403,"status_msg":"no"}`, string(object))
}

// TestRetryRulePhase4OmittedWhenNil asserts a rule without phase4 does not emit
// the field (pointer + omitempty), keeping stored rules clean.
func TestRetryRulePhase4OmittedWhenNil(t *testing.T) {
	data, err := common.Marshal(RetryRule{Description: "x"})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "phase4_retry_action")
}
