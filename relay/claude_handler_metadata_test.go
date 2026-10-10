package relay

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestFillClaudeMetadataUserID(t *testing.T) {
	const apiKey = "abcdef0123456789XYZ789"

	cases := []struct {
		name             string
		body             string
		apiKey           string
		wantDeviceID     string // "" means metadata.user_id must not exist
		wantInnerSession string // expected session_id embedded in the synthesized user_id blob
		wantRawUserID    string // asserted verbatim when non-empty (client value preserved)
		wantSessionID    string // top-level metadata.session_id, asserted only when non-empty
		wantUnchanged    bool
	}{
		{
			name:         "no metadata at all synthesizes claude-code style user_id",
			body:         `{"model":"claude-opus-5-5","max_tokens":1024}`,
			apiKey:       apiKey,
			wantDeviceID: "XYZ789",
		},
		{
			name:             "synthesized user_id reuses existing session_id",
			body:             `{"metadata":{"session_id":"sess-1"}}`,
			apiKey:           apiKey,
			wantDeviceID:     "XYZ789",
			wantInnerSession: "sess-1",
			wantSessionID:    "sess-1",
		},
		{
			name:          "client user_id is preserved verbatim",
			body:          `{"metadata":{"user_id":"{\"device_id\":\"abc\",\"session_id\":\"s1\"}"}}`,
			apiKey:        apiKey,
			wantRawUserID: `{"device_id":"abc","session_id":"s1"}`,
			wantUnchanged: true,
		},
		{
			name:         "empty client user_id is treated as missing",
			body:         `{"metadata":{"user_id":"  "}}`,
			apiKey:       apiKey,
			wantDeviceID: "XYZ789",
		},
		{
			name:          "short api key leaves body unchanged",
			body:          `{"model":"claude-opus-5-5"}`,
			apiKey:        "abc",
			wantUnchanged: true,
		},
		{
			name:          "empty api key leaves body unchanged",
			body:          `{"model":"claude-opus-5-5"}`,
			apiKey:        "",
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

			if tc.wantRawUserID != "" {
				assert.Equal(t, tc.wantRawUserID, userID.String())
			}

			if tc.wantDeviceID == "" {
				if !tc.wantUnchanged {
					assert.False(t, userID.Exists(), "metadata.user_id should not be set")
				}
			} else {
				// user_id is a JSON string in the Claude Code shape; parse it.
				inner := gjson.Parse(userID.String())
				assert.Equal(t, tc.wantDeviceID, inner.Get("device_id").String())
				assert.True(t, inner.Get("account_uuid").Exists(), "account_uuid field should exist")
				assert.Equal(t, tc.wantInnerSession, inner.Get("session_id").String())
			}

			if tc.wantSessionID != "" {
				assert.Equal(t, tc.wantSessionID, gjson.GetBytes(out, "metadata.session_id").String())
			}
		})
	}
}

func TestFillClaudeMetadataSessionID(t *testing.T) {
	const (
		clientIP  = "1.2.3.4"
		userAgent = "claude-cli/2.1.270"
		apiKeyID  = "key123456"
		sep       = "\x1f"
	)

	t.Run("session header wins and is combined with model and stable system", func(t *testing.T) {
		// Message content must be ignored for the conversation identity when the
		// header is present.
		body := `{"model":"claude-opus-5-5","system":[{"type":"text","text":"P","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`
		out, err := fillClaudeMetadataSessionID([]byte(body), " s1 ", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)

		want := claudeSessionHash(strings.Join([]string{"claude-opus-5-5", "P", "s1"}, sep))
		got := gjson.GetBytes(out, "metadata.session_id").String()
		assert.Equal(t, want, got)
		assert.Len(t, got, 16)
	})

	t.Run("falls back to ephemeral cache_control content", func(t *testing.T) {
		body := `{"model":"m1","system":[{"type":"text","text":"SYS","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"MSGCACHE","cache_control":{"type":"ephemeral"}}]}]}`
		out, err := fillClaudeMetadataSessionID([]byte(body), "", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)

		want := claudeSessionHash(strings.Join([]string{"m1", "SYS", "SYSMSGCACHE"}, sep))
		assert.Equal(t, want, gjson.GetBytes(out, "metadata.session_id").String())
	})

	t.Run("identity fallback is stable across turns", func(t *testing.T) {
		turn1 := `{"model":"m1","messages":[{"role":"user","content":"hello"}]}`
		turn2 := `{"model":"m1","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"more"}]}`

		out1, err := fillClaudeMetadataSessionID([]byte(turn1), "", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)
		out2, err := fillClaudeMetadataSessionID([]byte(turn2), "", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)

		want := claudeSessionHash(strings.Join([]string{"m1", "", clientIP + userAgent + apiKeyID}, sep))
		assert.Equal(t, want, gjson.GetBytes(out1, "metadata.session_id").String())
		assert.Equal(t, want, gjson.GetBytes(out2, "metadata.session_id").String())
	})

	t.Run("model switch changes the session_id for the same conversation", func(t *testing.T) {
		opus := `{"model":"claude-opus-5-5","system":[{"type":"text","text":"P","cache_control":{"type":"ephemeral"}}]}`
		sonnet := `{"model":"claude-sonnet-5","system":[{"type":"text","text":"P","cache_control":{"type":"ephemeral"}}]}`

		o1, err := fillClaudeMetadataSessionID([]byte(opus), "s1", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)
		o2, err := fillClaudeMetadataSessionID([]byte(sonnet), "s1", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)

		assert.NotEqual(t,
			gjson.GetBytes(o1, "metadata.session_id").String(),
			gjson.GetBytes(o2, "metadata.session_id").String())
	})

	t.Run("dynamic system tail after the breakpoint does not change session_id", func(t *testing.T) {
		a := `{"model":"m1","system":[{"type":"text","text":"STABLE","cache_control":{"type":"ephemeral"}},{"type":"text","text":"TAIL-A"}]}`
		b := `{"model":"m1","system":[{"type":"text","text":"STABLE","cache_control":{"type":"ephemeral"}},{"type":"text","text":"TAIL-B"}]}`

		oa, err := fillClaudeMetadataSessionID([]byte(a), "s1", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)
		ob, err := fillClaudeMetadataSessionID([]byte(b), "s1", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)

		assert.Equal(t,
			gjson.GetBytes(oa, "metadata.session_id").String(),
			gjson.GetBytes(ob, "metadata.session_id").String())
	})

	t.Run("client with user_id gets no session injection", func(t *testing.T) {
		// Claude Code already carries its session inside user_id, so metadata is
		// left untouched even though metadata.session_id is absent.
		body := `{"model":"m1","metadata":{"user_id":"{\"device_id\":\"d\",\"session_id\":\"cc\"}"},"system":[{"type":"text","text":"P","cache_control":{"type":"ephemeral"}}]}`
		out, err := fillClaudeMetadataSessionID([]byte(body), "s1", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)

		assert.False(t, gjson.GetBytes(out, "metadata.session_id").Exists(), "session_id must not be injected when user_id present")
		assert.JSONEq(t, body, string(out))
	})

	t.Run("existing session_id is preserved", func(t *testing.T) {
		body := `{"model":"m1","metadata":{"session_id":"keep-me"},"messages":[{"role":"user","content":"hi"}]}`
		out, err := fillClaudeMetadataSessionID([]byte(body), "hdr", clientIP, userAgent, apiKeyID)
		require.NoError(t, err)

		assert.Equal(t, "keep-me", gjson.GetBytes(out, "metadata.session_id").String())
	})
}

// TestClaudeMetadataInjectionWhenMetadataAbsent mirrors the production call
// order on a body with no metadata object at all, confirming both fields are
// created and the user_id blob's session_id matches metadata.session_id.
func TestClaudeMetadataInjectionWhenMetadataAbsent(t *testing.T) {
	const apiKey = "abcdef0123456789XYZ789"
	body := []byte(`{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}]}`)

	out, err := fillClaudeMetadataSessionID(body, "", "1.2.3.4", "pi/1.0", apiKey)
	require.NoError(t, err)
	out, err = fillClaudeMetadataUserID(out, apiKey)
	require.NoError(t, err)

	sessionID := gjson.GetBytes(out, "metadata.session_id").String()
	require.Len(t, sessionID, 16)

	inner := gjson.Parse(gjson.GetBytes(out, "metadata.user_id").String())
	assert.Equal(t, "XYZ789", inner.Get("device_id").String())
	assert.Equal(t, sessionID, inner.Get("session_id").String())
	assert.True(t, inner.Get("account_uuid").Exists())
}
