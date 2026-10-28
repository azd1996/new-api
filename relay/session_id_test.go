package relay

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

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
		name       string
		req        *dto.OpenAIResponsesRequest
		wantValue  string
		wantSource string
	}{
		{"prompt_cache_key wins", &dto.OpenAIResponsesRequest{
			PromptCacheKey: json.RawMessage(`"pck-1"`),
			ClientMetadata: json.RawMessage(`{"session_id":"cm-1"}`),
		}, "pck-1", "prompt_cache_key"},
		{"client_metadata fallback", &dto.OpenAIResponsesRequest{
			ClientMetadata: json.RawMessage(`{"session_id":"cm-1"}`),
		}, "cm-1", "client_metadata.session_id"},
		{"none", &dto.OpenAIResponsesRequest{}, "", ""},
		{"nil", nil, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, source := responsesSessionID(tc.req)
			assert.Equal(t, tc.wantValue, value)
			assert.Equal(t, tc.wantSource, source)
		})
	}
}

func TestRecordRequestSessionID(t *testing.T) {
	cases := []struct {
		name       string
		value      string
		source     string
		wantValue  string
		wantSource string
	}{
		{"value with source", "sess-1", "prompt_cache_key", "sess-1", "prompt_cache_key"},
		{"empty -> notfound, no source", "", "prompt_cache_key", sessionIDNotFound, ""},
		{"blank -> notfound", "   ", "metadata.user_id", sessionIDNotFound, ""},
		{"value without source", "sess-2", "", "sess-2", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			recordRequestSessionID(c, tc.value, tc.source)
			assert.Equal(t, tc.wantValue, common.GetContextKeyString(c, constant.ContextKeySessionID))
			assert.Equal(t, tc.wantSource, common.GetContextKeyString(c, constant.ContextKeySessionIDSource))
		})
	}
}
