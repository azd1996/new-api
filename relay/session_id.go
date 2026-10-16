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

// recordRequestSessionID stashes the request's session identifier and its
// source label on the gin context so the consume/error log can persist them
// under other.session_id / other.session_id_source. Only session-capable relay
// paths call this; an empty value is normalized to the "notfound" sentinel (and
// no source is recorded).
func recordRequestSessionID(c *gin.Context, sessionID, source string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		common.SetContextKey(c, constant.ContextKeySessionID, sessionIDNotFound)
		return
	}
	common.SetContextKey(c, constant.ContextKeySessionID, sessionID)
	if source = strings.TrimSpace(source); source != "" {
		common.SetContextKey(c, constant.ContextKeySessionIDSource, source)
	}
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

// responsesSessionID resolves the session identifier and its source for an
// OpenAI Responses request: prompt_cache_key first, then
// client_metadata.session_id.
func responsesSessionID(req *dto.OpenAIResponsesRequest) (string, string) {
	if req == nil {
		return "", ""
	}
	if len(req.PromptCacheKey) > 0 {
		if v := strings.TrimSpace(gjson.ParseBytes(req.PromptCacheKey).String()); v != "" {
			return v, "prompt_cache_key"
		}
	}
	if len(req.ClientMetadata) > 0 {
		if v := strings.TrimSpace(gjson.GetBytes(req.ClientMetadata, "session_id").String()); v != "" {
			return v, "client_metadata.session_id"
		}
	}
	return "", ""
}
