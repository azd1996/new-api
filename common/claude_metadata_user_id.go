package common

import (
	"fmt"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const ClaudeDerivedDeviceIDLen = 6

type claudeMetadataUserID struct {
	DeviceID    string `json:"device_id"`
	AccountUUID string `json:"account_uuid"`
	SessionID   string `json:"session_id"`
}

// PrepareClaudeMetadataUserID synthesizes the Claude Code-style metadata.user_id
// used by channel selection and relay. It returns whether a value was added.
func PrepareClaudeMetadataUserID(jsonData []byte, apiKey, headerSessionID, clientIP, userAgent string) ([]byte, bool, error) {
	if existing := gjson.GetBytes(jsonData, "metadata.user_id"); existing.Exists() && strings.TrimSpace(existing.String()) != "" {
		return jsonData, false, nil
	}
	if len(apiKey) < ClaudeDerivedDeviceIDLen {
		return jsonData, false, nil
	}
	sessionID := ClaudeSessionHash(DeriveClaudeSessionSeed(jsonData, headerSessionID, clientIP, userAgent, apiKey))
	encoded, err := Marshal(claudeMetadataUserID{
		DeviceID:  apiKey[len(apiKey)-ClaudeDerivedDeviceIDLen:],
		SessionID: sessionID,
	})
	if err != nil {
		return jsonData, false, err
	}
	out, err := sjson.SetBytes(jsonData, "metadata.user_id", string(encoded))
	if err != nil {
		return jsonData, false, err
	}
	return out, true, nil
}

// ClaudeSessionHash reduces a trimmed session seed to 16 hex characters.
// Empty seeds stay empty so callers can skip injection.
func ClaudeSessionHash(seed string) string {
	normalized := strings.TrimSpace(seed)
	if normalized == "" {
		return ""
	}
	return fmt.Sprintf("%016x", xxhash.Sum64String(normalized))
}

// DeriveClaudeSessionSeed combines the original model, stable system prefix and
// conversation identity. Moving message breakpoints and dynamic system tails
// must not split one conversation into separate sessions.
func DeriveClaudeSessionSeed(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string) string {
	conversation := claudeConversationSeed(jsonData, headerSessionID, clientIP, userAgent, apiKeyID)
	model := gjson.GetBytes(jsonData, "model").String()
	systemPrefix := claudeStableSystemPrefix(jsonData)
	if model == "" && systemPrefix == "" && conversation == "" {
		return ""
	}
	return strings.Join([]string{model, systemPrefix, conversation}, "\x1f")
}

func claudeConversationSeed(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string) string {
	if s := strings.TrimSpace(headerSessionID); s != "" {
		return s
	}
	if s := claudeFirstMessageSeed(jsonData); strings.TrimSpace(s) != "" {
		return apiKeyID + "\x1f" + s
	}
	return clientIP + userAgent + apiKeyID
}

// claudeStableSystemPrefix includes system blocks through the first ephemeral
// breakpoint. Without a breakpoint, only the first block anchors the session.
func claudeStableSystemPrefix(jsonData []byte) string {
	sys := gjson.GetBytes(jsonData, "system")
	if sys.Type == gjson.String {
		return sys.String()
	}
	if !sys.IsArray() {
		return ""
	}
	blocks := sys.Array()
	var sb strings.Builder
	for _, block := range blocks {
		if text := block.Get("text"); text.Exists() {
			sb.WriteString(text.String())
		} else {
			sb.WriteString(block.Raw)
		}
		if block.Get("cache_control.type").String() == "ephemeral" {
			return sb.String()
		}
	}
	if len(blocks) == 0 {
		return ""
	}
	if text := blocks[0].Get("text"); text.Exists() {
		return text.String()
	}
	return blocks[0].Raw
}

// claudeFirstMessageSeed ignores cache_control: clients move the breakpoint
// between messages as a conversation grows.
func claudeFirstMessageSeed(jsonData []byte) string {
	content := gjson.GetBytes(jsonData, "messages.0.content")
	if content.Type == gjson.String {
		return content.String()
	}
	if !content.IsArray() {
		return ""
	}
	var sb strings.Builder
	for _, block := range content.Array() {
		if text := block.Get("text"); text.Exists() {
			sb.WriteString(text.String())
			continue
		}
		raw, err := sjson.Delete(block.Raw, "cache_control")
		if err != nil {
			raw = block.Raw
		}
		sb.WriteString(raw)
	}
	return sb.String()
}
