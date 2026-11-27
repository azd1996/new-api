package common

import (
	"fmt"
	"net/http"
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
	return PrepareClaudeMetadataUserIDWithConfig(jsonData, apiKey, headerSessionID, clientIP, userAgent, nil, nil)
}

// PrepareClaudeMetadataUserIDWithConfig additionally consults a customer's
// session identity extraction configuration before falling back to the legacy
// heuristics. A nil config preserves the legacy behavior.
func PrepareClaudeMetadataUserIDWithConfig(jsonData []byte, apiKey, headerSessionID, clientIP, userAgent string, headers http.Header, cfg *SessionIdentityConfig) ([]byte, bool, error) {
	out, synthesized, _, err := PrepareClaudeMetadataUserIDWithResolution(jsonData, apiKey, headerSessionID, clientIP, userAgent, headers, cfg)
	return out, synthesized, err
}

// PrepareClaudeMetadataUserIDWithResolution also reports which identity source
// tier produced the session id (for example "header", "custom:regex(path)",
// "legacy:first_message" or "legacy:client_fingerprint"), so callers can
// persist the provenance next to the synthesized value.
func PrepareClaudeMetadataUserIDWithResolution(jsonData []byte, apiKey, headerSessionID, clientIP, userAgent string, headers http.Header, cfg *SessionIdentityConfig) ([]byte, bool, string, error) {
	if existing := gjson.GetBytes(jsonData, "metadata.user_id"); existing.Exists() && strings.TrimSpace(existing.String()) != "" {
		return jsonData, false, "", nil
	}
	if len(apiKey) < ClaudeDerivedDeviceIDLen {
		return jsonData, false, "", nil
	}
	resolution := ResolveSessionIdentity(jsonData, headerSessionID, clientIP, userAgent, apiKey, headers, cfg)
	if resolution.SessionID == "" {
		return jsonData, false, "", nil
	}
	encoded, err := Marshal(claudeMetadataUserID{
		DeviceID:  apiKey[len(apiKey)-ClaudeDerivedDeviceIDLen:],
		SessionID: resolution.SessionID,
	})
	if err != nil {
		return jsonData, false, "", err
	}
	out, err := sjson.SetBytes(jsonData, "metadata.user_id", string(encoded))
	if err != nil {
		return jsonData, false, "", err
	}
	return out, true, resolution.Source, nil
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
	return DeriveClaudeSessionSeedWithConfig(jsonData, headerSessionID, clientIP, userAgent, apiKeyID, nil, nil)
}

// DeriveClaudeSessionSeedWithConfig consults a customer's session identity
// extraction configuration before the legacy heuristics.
func DeriveClaudeSessionSeedWithConfig(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string, headers http.Header, cfg *SessionIdentityConfig) string {
	conversation, _, _ := claudeConversationSeed(jsonData, headerSessionID, clientIP, userAgent, apiKeyID, headers, cfg)
	model := gjson.GetBytes(jsonData, "model").String()
	systemPrefix := claudeStableSystemPrefix(jsonData)
	if model == "" && systemPrefix == "" && conversation == "" {
		return ""
	}
	return strings.Join([]string{model, systemPrefix, conversation}, "\x1f")
}

// ResolveSessionIdentity runs the full fallback pipeline and reports both the
// final session id and the source tier that produced it. It is shared by the
// relay metadata synthesis and the admin dry-run endpoint so both report the
// same values production would use.
func ResolveSessionIdentity(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string, headers http.Header, cfg *SessionIdentityConfig) SessionIdentityResolution {
	if existing := gjson.GetBytes(jsonData, "metadata.user_id"); existing.Exists() && strings.TrimSpace(existing.String()) != "" {
		return SessionIdentityResolution{Source: "client", RawValue: truncateSessionIdentityValue(existing.String())}
	}
	conversation, source, raw := claudeConversationSeed(jsonData, headerSessionID, clientIP, userAgent, apiKeyID, headers, cfg)
	model := gjson.GetBytes(jsonData, "model").String()
	systemPrefix := claudeStableSystemPrefix(jsonData)
	if conversation == "" && cfg != nil && cfg.Matches(model) && cfg.OnEmpty == SessionIdentityOnEmptyNone {
		// on_empty=none must not synthesize an identity from the model and
		// system prefix alone.
		return SessionIdentityResolution{Source: "none", Reason: "on_empty_none"}
	}
	if model == "" && systemPrefix == "" && conversation == "" {
		return SessionIdentityResolution{Source: "none", Reason: "no_fallback_context"}
	}
	seed := strings.Join([]string{model, systemPrefix, conversation}, "\x1f")
	return SessionIdentityResolution{
		SessionID: ClaudeSessionHash(seed),
		Source:    source,
		RawValue:  raw,
	}
}

// claudeConversationSeed picks the conversation identity by priority: session
// header, then a customer's custom extraction (if configured and matching),
// then the first message (unless disabled), then a request-wide fallback. The
// returned raw value is only populated for custom/header sources; legacy
// sources would otherwise leak prompt text into logs.
func claudeConversationSeed(jsonData []byte, headerSessionID, clientIP, userAgent, apiKeyID string, headers http.Header, cfg *SessionIdentityConfig) (string, string, string) {
	if s := strings.TrimSpace(headerSessionID); s != "" {
		return s, "header", truncateSessionIdentityValue(s)
	}
	model := gjson.GetBytes(jsonData, "model").String()
	if cfg != nil && cfg.Matches(model) {
		raw, source := ExtractSessionIdentity(cfg, jsonData, headers)
		if raw != "" {
			return NormalizeSessionIdentityValue(raw, cfg.Transform), "custom:" + source, truncateSessionIdentityValue(raw)
		}
		switch cfg.OnEmpty {
		case SessionIdentityOnEmptyNone:
			return "", "", ""
		case SessionIdentityOnEmptyStable:
			// Skip the rotating first-message heuristic; use the fingerprint.
		default:
			if !cfg.DisableFirstMessageFallback {
				if s := claudeFirstMessageSeed(jsonData); strings.TrimSpace(s) != "" {
					return apiKeyID + "\x1f" + s, "legacy:first_message", ""
				}
			}
		}
	} else if s := claudeFirstMessageSeed(jsonData); strings.TrimSpace(s) != "" {
		return apiKeyID + "\x1f" + s, "legacy:first_message", ""
	}
	if fingerprint := clientIP + userAgent + apiKeyID; fingerprint != "" {
		return fingerprint, "legacy:client_fingerprint", ""
	}
	return "", "", ""
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
