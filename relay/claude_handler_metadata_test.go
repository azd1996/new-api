package relay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestFillClaudeMetadataUserID(t *testing.T) {
	const apiKey = "abcdef0123456789XYZ789"

	cases := []struct {
		name           string
		body           string
		apiKey         string
		wantUserID     string // "" means metadata.user_id must not exist
		wantSessionID  string // asserted only when non-empty
		wantUnchanged  bool
	}{
		{
			name:       "no metadata at all is created from key suffix",
			body:       `{"model":"claude-opus-5-5","max_tokens":1024}`,
			apiKey:     apiKey,
			wantUserID: "XYZ789",
		},
		{
			name:          "existing metadata without user_id keeps session_id",
			body:          `{"metadata":{"session_id":"sess-1"}}`,
			apiKey:        apiKey,
			wantUserID:    "XYZ789",
			wantSessionID: "sess-1",
		},
		{
			name:          "client user_id is preserved",
			body:          `{"metadata":{"user_id":"strict_user_id_12345","session_id":"sess-1"}}`,
			apiKey:        apiKey,
			wantUserID:    "strict_user_id_12345",
			wantSessionID: "sess-1",
			wantUnchanged: true,
		},
		{
			name:       "empty client user_id is treated as missing",
			body:       `{"metadata":{"user_id":"  "}}`,
			apiKey:     apiKey,
			wantUserID: "XYZ789",
		},
		{
			name:          "short api key leaves body unchanged",
			body:          `{"model":"claude-opus-5-5"}`,
			apiKey:        "abc",
			wantUserID:    "",
			wantUnchanged: true,
		},
		{
			name:          "empty api key leaves body unchanged",
			body:          `{"model":"claude-opus-5-5"}`,
			apiKey:        "",
			wantUserID:    "",
			wantUnchanged: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := fillClaudeMetadataUserID([]byte(tc.body), tc.apiKey)
			require.NoError(t, err)

			if tc.wantUnchanged {
				assert.JSONEq(t, tc.body, string(out))
			}

			userID := gjson.GetBytes(out, "metadata.user_id")
			if tc.wantUserID == "" {
				assert.False(t, userID.Exists(), "metadata.user_id should not be set")
			} else {
				assert.Equal(t, tc.wantUserID, userID.String())
			}

			if tc.wantSessionID != "" {
				assert.Equal(t, tc.wantSessionID, gjson.GetBytes(out, "metadata.session_id").String())
			}
		})
	}
}
