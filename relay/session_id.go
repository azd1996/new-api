package relay

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// sessionIDNotFound is the sentinel stored for a session-capable request that
// carried no resolvable session identifier. Writing a concrete value (instead
// of leaving the key absent) keeps "no session id" requests searchable.
const sessionIDNotFound = "notfound"

// recordRequestSessionID stashes the request's session identifier on the gin
// context so the consume/error log can persist it under other.session_id.
// Only session-capable relay paths (Claude native, OpenAI chat/responses)
// call this; an empty value is normalized to the "notfound" sentinel.
func recordRequestSessionID(c *gin.Context, sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = sessionIDNotFound
	}
	common.SetContextKey(c, constant.ContextKeySessionID, sessionID)
}

// claudeSessionID reads the session_id embedded in the final metadata.user_id
// blob (either the client's own value or the one we injected). Returns "" when
// the field is absent or the blob is not Claude Code-shaped.
func claudeSessionID(jsonData []byte) string {
	raw := gjson.GetBytes(jsonData, "metadata.user_id")
	if !raw.Exists() {
		return ""
	}
	var blob claudeMetadataUserID
	if err := common.UnmarshalJsonStr(raw.String(), &blob); err != nil {
		return ""
	}
	return strings.TrimSpace(blob.SessionID)
}

// responsesSessionID resolves the session identifier for an OpenAI Responses
// request: prompt_cache_key first, then client_metadata.session_id.
func responsesSessionID(req *dto.OpenAIResponsesRequest) string {
	if req == nil {
		return ""
	}
	if len(req.PromptCacheKey) > 0 {
		if v := strings.TrimSpace(gjson.ParseBytes(req.PromptCacheKey).String()); v != "" {
			return v
		}
	}
	if len(req.ClientMetadata) > 0 {
		if v := strings.TrimSpace(gjson.GetBytes(req.ClientMetadata, "session_id").String()); v != "" {
			return v
		}
	}
	return ""
}
