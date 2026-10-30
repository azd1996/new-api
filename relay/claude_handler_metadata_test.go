package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	testClientIP  = "1.2.3.4"
	testUserAgent = "claude-cli/2.1.270"
	testAPIKey    = "abcdef0123456789XYZ789" // last 6 chars = XYZ789
	seedSep       = "\x1f"
)

func TestClaudeHelperRecordsFinalSession(t *testing.T) {
	for _, tc := range []struct {
		name, metadata, rewrite, wantSession, wantSource string
		param                                            bool
	}{
		{name: "client", metadata: `,"metadata":{"user_id":"{\"session_id\":\"client\"}"}`, wantSession: "client", wantSource: "metadata.user_id"},
		{name: "injected", wantSource: "content-fallback"},
		{name: "retry only", rewrite: `{"session_id":"retry"}`, wantSession: "retry", wantSource: "content-fallback"},
		{name: "param overrides client", metadata: `,"metadata":{"user_id":"{\"session_id\":\"client\"}"}`, rewrite: `{"session_id":"param"}`, param: true, wantSession: "param", wantSource: "metadata.user_id"},
		{name: "retry removes session", rewrite: `{}`, wantSession: sessionIDNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var outbound []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				outbound, _ = io.ReadAll(r.Body)
				// Stop before settlement; this fixture never accesses a database.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"test rejection"}}`))
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-sonnet-4")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-test")
			common.SetContextKey(c, constant.ContextKeyTokenKey, testAPIKey)
			var request dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-sonnet-4","max_tokens":16,"messages":[{"role":"user","content":"hi"}]`+tc.metadata+`}`, &request))
			info := &relaycommon.RelayInfo{Request: &request, RelayFormat: types.RelayFormatClaude, OriginModelName: request.Model, RequestURLPath: "/v1/messages"}
			if tc.rewrite != "" {
				if tc.param {
					common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{
						"operations": []map[string]any{{"mode": "set", "path": "metadata.user_id", "value": tc.rewrite}},
					})
				} else {
					info.PendingRetryRewrite = []map[string]any{{"mode": "set", "path": "metadata.user_id", "value": tc.rewrite}}
				}
			}
			apiErr := ClaudeHelper(c, info)
			require.NotNil(t, apiErr)
			require.NotEmpty(t, outbound, "request must reach the upstream fixture")
			wantSession := tc.wantSession
			if wantSession == "" {
				wantSession = claudeSessionID(outbound)
				require.NotEmpty(t, wantSession)
			}
			assert.Equal(t, wantSession, common.GetContextKeyString(c, constant.ContextKeySessionID))
			assert.Equal(t, tc.wantSource, common.GetContextKeyString(c, constant.ContextKeySessionIDSource))
			assert.Empty(t, info.PendingRetryRewrite)
		})
	}
}

func TestFillClaudeMetadataUserID(t *testing.T) {
	cases := []struct {
		name             string
		body             string
		apiKey           string
		wantRawUserID    string // asserted verbatim when the client value is preserved
		wantUserIDExists bool
		wantDeviceID     string
	}{
		{
			name:             "no metadata synthesizes claude-code style blob",
			body:             `{"model":"m1","messages":[{"role":"user","content":"hi"}]}`,
			apiKey:           testAPIKey,
			wantUserIDExists: true,
			wantDeviceID:     "XYZ789",
		},
		{
			name:          "client user_id is preserved verbatim",
			body:          `{"metadata":{"user_id":"{\"device_id\":\"abc\",\"session_id\":\"s1\"}"}}`,
			apiKey:        testAPIKey,
			wantRawUserID: `{"device_id":"abc","session_id":"s1"}`,
		},
		{
			name:             "empty client user_id is treated as missing",
			body:             `{"model":"m1","metadata":{"user_id":"  "}}`,
			apiKey:           testAPIKey,
			wantUserIDExists: true,
			wantDeviceID:     "XYZ789",
		},
		{
			name:   "short api key leaves body unchanged",
			body:   `{"model":"m1"}`,
			apiKey: "abc",
		},
		{
			name:   "empty api key leaves body unchanged",
			body:   `{"model":"m1"}`,
			apiKey: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := fillClaudeMetadataUserID([]byte(tc.body), tc.apiKey, "", testClientIP, testUserAgent)
			require.NoError(t, err)

			userID := gjson.GetBytes(out, "metadata.user_id")

			if tc.wantRawUserID != "" {
				assert.Equal(t, tc.wantRawUserID, userID.String())
				return
			}
			if !tc.wantUserIDExists {
				assert.JSONEq(t, tc.body, string(out))
				return
			}

			inner := gjson.Parse(userID.String())
			assert.Equal(t, tc.wantDeviceID, inner.Get("device_id").String())
			assert.True(t, inner.Get("account_uuid").Exists(), "account_uuid field should exist")
			assert.Len(t, inner.Get("session_id").String(), 16, "embedded session_id is a 16-hex hash")
			// A top-level metadata.session_id must never be written (upstream rejects it).
			assert.False(t, gjson.GetBytes(out, "metadata.session_id").Exists())
		})
	}
}

// innerSessionID runs the synthesis and returns the session_id embedded in the
// generated metadata.user_id blob.
func innerSessionID(t *testing.T, body, header string) string {
	t.Helper()
	out, err := fillClaudeMetadataUserID([]byte(body), testAPIKey, header, testClientIP, testUserAgent)
	require.NoError(t, err)
	return gjson.Parse(gjson.GetBytes(out, "metadata.user_id").String()).Get("session_id").String()
}

func TestClaudeSessionDerivation(t *testing.T) {
	t.Run("header combines with model and stable system prefix", func(t *testing.T) {
		body := `{"model":"claude-opus-5-5","system":[{"type":"text","text":"P","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`
		want := claudeSessionHash(strings.Join([]string{"claude-opus-5-5", "P", "s1"}, seedSep))
		assert.Equal(t, want, innerSessionID(t, body, " s1 "))
	})

	t.Run("falls back to ephemeral cache_control content", func(t *testing.T) {
		body := `{"model":"m1","system":[{"type":"text","text":"SYS","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"MSGCACHE","cache_control":{"type":"ephemeral"}}]}]}`
		want := claudeSessionHash(strings.Join([]string{"m1", "SYS", "SYSMSGCACHE"}, seedSep))
		assert.Equal(t, want, innerSessionID(t, body, ""))
	})

	t.Run("identity fallback is stable across turns", func(t *testing.T) {
		turn1 := `{"model":"m1","messages":[{"role":"user","content":"hello"}]}`
		turn2 := `{"model":"m1","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"more"}]}`
		want := claudeSessionHash(strings.Join([]string{"m1", "", testClientIP + testUserAgent + testAPIKey}, seedSep))
		assert.Equal(t, want, innerSessionID(t, turn1, ""))
		assert.Equal(t, want, innerSessionID(t, turn2, ""))
	})

	t.Run("model switch changes the session for the same conversation", func(t *testing.T) {
		opus := `{"model":"claude-opus-5-5","system":[{"type":"text","text":"P","cache_control":{"type":"ephemeral"}}]}`
		sonnet := `{"model":"claude-sonnet-5","system":[{"type":"text","text":"P","cache_control":{"type":"ephemeral"}}]}`
		assert.NotEqual(t, innerSessionID(t, opus, "s1"), innerSessionID(t, sonnet, "s1"))
	})

	t.Run("dynamic system tail after the breakpoint does not change the session", func(t *testing.T) {
		a := `{"model":"m1","system":[{"type":"text","text":"STABLE","cache_control":{"type":"ephemeral"}},{"type":"text","text":"TAIL-A"}]}`
		b := `{"model":"m1","system":[{"type":"text","text":"STABLE","cache_control":{"type":"ephemeral"}},{"type":"text","text":"TAIL-B"}]}`
		assert.Equal(t, innerSessionID(t, a, "s1"), innerSessionID(t, b, "s1"))
	})
}
