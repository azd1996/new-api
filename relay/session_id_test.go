package relay

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestClaudeSessionID(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"blob with session", `{"metadata":{"user_id":"{\"device_id\":\"d\",\"session_id\":\"sess-abc\"}"}}`, "sess-abc"},
		{"blob without session", `{"metadata":{"user_id":"{\"device_id\":\"d\"}"}}`, ""},
		{"non-blob string user_id", `{"metadata":{"user_id":"plain-string"}}`, ""},
		{"no metadata", `{"model":"claude"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, claudeSessionID([]byte(tc.body)))
		})
	}
}

func TestResponsesSessionID(t *testing.T) {
	cases := []struct {
		name string
		req  *dto.OpenAIResponsesRequest
		want string
	}{
		{"prompt_cache_key wins", &dto.OpenAIResponsesRequest{
			PromptCacheKey: json.RawMessage(`"pck-1"`),
			ClientMetadata: json.RawMessage(`{"session_id":"cm-1"}`),
		}, "pck-1"},
		{"client_metadata fallback", &dto.OpenAIResponsesRequest{
			ClientMetadata: json.RawMessage(`{"session_id":"cm-1"}`),
		}, "cm-1"},
		{"none", &dto.OpenAIResponsesRequest{}, ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, responsesSessionID(tc.req))
		})
	}
}

func TestRecordRequestSessionIDFallsBackToNotFound(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"value kept", "sess-1", "sess-1"},
		{"empty -> notfound", "", sessionIDNotFound},
		{"blank -> notfound", "   ", sessionIDNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			recordRequestSessionID(c, tc.input)
			assert.Equal(t, tc.want, common.GetContextKeyString(c, constant.ContextKeySessionID))
		})
	}
}
