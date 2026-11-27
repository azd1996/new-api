package common

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/tidwall/gjson"
)

// Session identity extraction lets a single customer configure how the
// fallback Claude session id is derived from its requests. The default
// fallback uses the first user message, which rotates for clients that keep
// their history in the system prompt and send only the latest turn.

const (
	// SessionIdentitySourceClientMetadata reads the client-provided
	// metadata.user_id blob (optionally decoding an inner field).
	SessionIdentitySourceClientMetadata = "metadata_user_id"
	// SessionIdentitySourceHeader reads an arbitrary request header.
	SessionIdentitySourceHeader = "request_header"
	// SessionIdentitySourceGjson reads any body field via a gjson path.
	SessionIdentitySourceGjson = "gjson"
	// SessionIdentitySourceRegex applies a regular expression to the text at a
	// gjson path. The regex only ever runs on that single field, never on the
	// whole body.
	SessionIdentitySourceRegex = "regex"
)

const (
	SessionIdentityTransformNone    = ""
	SessionIdentityTransformXXHash  = "xxhash64"
	SessionIdentityTransformSHA1_16 = "sha1_16"
)

const (
	// SessionIdentityOnEmptyLegacy (the default) falls back to the legacy
	// first-message heuristic and then the client fingerprint.
	SessionIdentityOnEmptyLegacy = "legacy"
	// SessionIdentityOnEmptyStable skips the first-message heuristic and uses
	// only the client fingerprint.
	SessionIdentityOnEmptyStable = "stable"
	// SessionIdentityOnEmptyNone injects no synthesized session id at all.
	SessionIdentityOnEmptyNone = "none"
)

// MaxSessionIdentityValueLength bounds the raw value so a misconfigured regex
// cannot push a whole prompt into session/affinity cache keys.
const MaxSessionIdentityValueLength = 256

// Long GJSON values are eligible for the final session hash only.
const maxSessionIdentityGJSONBytes = 1 << 20

// SessionIdentitySource describes one place a session id can be extracted from.
type SessionIdentitySource struct {
	Type string `json:"type"`
	// Key names the header for request_header sources.
	Key string `json:"key,omitempty"`
	// Path is the gjson path for gjson/regex sources.
	Path string `json:"path,omitempty"`
	// JSONStringField optionally decodes an inner field when the extracted
	// value is itself a JSON string (e.g. metadata.user_id holds a Claude
	// Code-shaped blob containing session_id).
	JSONStringField string `json:"json_string_field,omitempty"`
	// Pattern is the regular expression for regex sources. It is applied only
	// to the text selected by Path.
	Pattern string `json:"pattern,omitempty"`
	// CaptureGroup selects which capture group to use (0 = whole match).
	CaptureGroup int `json:"capture_group,omitempty"`
	// Nil inherits the customer regex. An explicit empty string disables validation.
	ValueRegex *string `json:"value_regex,omitempty"`
}

// SessionIdentityConfig is the per-customer extraction configuration.
type SessionIdentityConfig struct {
	Enabled       bool                    `json:"enabled"`
	ModelPrefixes []string                `json:"model_prefixes"`
	Sources       []SessionIdentitySource `json:"sources"`
	Transform     string                  `json:"transform"`
	ValueRegex    string                  `json:"value_regex"`
	// OmitSystemPrefix applies only when a custom source wins.
	OmitSystemPrefix bool   `json:"omit_system_prefix,omitempty"`
	OnEmpty          string `json:"on_empty"`
	// DisableFirstMessageFallback skips the legacy messages[0] heuristic for
	// clients that send only the latest turn.
	DisableFirstMessageFallback bool `json:"disable_first_message_fallback"`
	// TTLSeconds overrides the affinity binding TTL (0 = global default).
	TTLSeconds int `json:"ttl_seconds"`
}

// Matches reports whether modelName is served by this configuration. Empty
// prefixes match every model; prefixes are literal and case-insensitive.
func (c *SessionIdentityConfig) Matches(model string) bool {
	if c == nil || !c.Enabled {
		return false
	}
	if len(c.ModelPrefixes) == 0 {
		return true
	}
	lower := strings.ToLower(model)
	for _, prefix := range c.ModelPrefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" && strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// Clone returns a private copy with normalized, non-shared slices.
func (c *SessionIdentityConfig) Clone() *SessionIdentityConfig {
	if c == nil {
		return nil
	}
	out := &SessionIdentityConfig{
		Enabled:                     c.Enabled,
		Transform:                   c.Transform,
		ValueRegex:                  c.ValueRegex,
		OmitSystemPrefix:            c.OmitSystemPrefix,
		OnEmpty:                     c.OnEmpty,
		DisableFirstMessageFallback: c.DisableFirstMessageFallback,
		TTLSeconds:                  c.TTLSeconds,
	}
	if len(c.ModelPrefixes) > 0 {
		prefixes := make([]string, 0, len(c.ModelPrefixes))
		seen := make(map[string]bool, len(c.ModelPrefixes))
		for _, prefix := range c.ModelPrefixes {
			prefix = strings.TrimSpace(prefix)
			key := strings.ToLower(prefix)
			if prefix != "" && !seen[key] {
				seen[key] = true
				prefixes = append(prefixes, prefix)
			}
		}
		if len(prefixes) > 0 {
			out.ModelPrefixes = prefixes
		}
	}
	if len(c.Sources) > 0 {
		out.Sources = append([]SessionIdentitySource(nil), c.Sources...)
		for i, source := range out.Sources {
			if source.ValueRegex != nil {
				pattern := *source.ValueRegex
				out.Sources[i].ValueRegex = &pattern
			}
		}
	}
	return out
}

// SessionIdentityResolution is the outcome of the session identity pipeline.
type SessionIdentityResolution struct {
	// SessionID is the final 16-hex session id, or "" when nothing was
	// synthesized (client-managed or on_empty=none).
	SessionID string `json:"session_id"`
	// Source labels the tier that produced the identity:
	// client, header, custom:<type>(<path|key>), legacy:first_message,
	// legacy:client_fingerprint, or none.
	Source string `json:"source"`
	// RawValue is the pre-transform value for custom/header sources, bounded
	// to a short preview. It is empty for legacy tiers to avoid leaking the
	// prompt.
	RawValue string `json:"raw_value,omitempty"`
	// Reason explains why no session id was produced during a dry run.
	Reason string `json:"reason,omitempty"`
}

var sessionIdentityRegexCache sync.Map // map[string]*regexp.Regexp

func compileSessionIdentityRegex(pattern string) (*regexp.Regexp, bool) {
	if pattern == "" {
		return nil, false
	}
	if cached, ok := sessionIdentityRegexCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), true
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		// Cache the failure too so a broken pattern is only parsed once.
		sessionIdentityRegexCache.Store(pattern, (*regexp.Regexp)(nil))
		return nil, false
	}
	sessionIdentityRegexCache.Store(pattern, compiled)
	return compiled, true
}

func extractJSONStringField(value, field string) string {
	if field == "" || value == "" {
		return value
	}
	res := gjson.Get(value, field)
	if !res.Exists() {
		return ""
	}
	return strings.TrimSpace(res.String())
}

func sessionIdentitySourceLabel(source SessionIdentitySource) string {
	switch source.Type {
	case SessionIdentitySourceHeader:
		if source.Key != "" {
			return SessionIdentitySourceHeader + "(" + source.Key + ")"
		}
	case SessionIdentitySourceGjson, SessionIdentitySourceRegex:
		if source.Path != "" {
			return source.Type + "(" + source.Path + ")"
		}
	case SessionIdentitySourceClientMetadata:
		return SessionIdentitySourceClientMetadata
	}
	return source.Type
}

// ExtractSessionIdentity walks the configured sources in order and returns the
// first non-empty, valid raw value together with its source label. It performs
// no fallback: callers decide what happens when the result is empty.
func ExtractSessionIdentity(cfg *SessionIdentityConfig, body []byte, headers http.Header) (string, string) {
	if cfg == nil {
		return "", ""
	}
	for _, source := range cfg.Sources {
		value := ""
		switch source.Type {
		case SessionIdentitySourceClientMetadata:
			res := gjson.GetBytes(body, "metadata.user_id")
			if res.Exists() {
				value = extractJSONStringField(strings.TrimSpace(res.String()), source.JSONStringField)
			}
		case SessionIdentitySourceHeader:
			if source.Key != "" && headers != nil {
				value = strings.TrimSpace(headers.Get(source.Key))
			}
		case SessionIdentitySourceGjson:
			res := gjson.GetBytes(body, source.Path)
			if res.Exists() {
				switch res.Type {
				case gjson.String, gjson.Number, gjson.True, gjson.False:
					value = strings.TrimSpace(res.String())
				default:
					value = strings.TrimSpace(res.Raw)
				}
				value = extractJSONStringField(value, source.JSONStringField)
			}
		case SessionIdentitySourceRegex:
			if source.Pattern == "" || source.Path == "" {
				break
			}
			res := gjson.GetBytes(body, source.Path)
			if !res.Exists() {
				break
			}
			re, ok := compileSessionIdentityRegex(source.Pattern)
			if !ok || re == nil {
				break
			}
			match := re.FindStringSubmatch(res.String())
			if source.CaptureGroup < 0 || source.CaptureGroup >= len(match) {
				break
			}
			value = strings.TrimSpace(match[source.CaptureGroup])
		}
		maxBytes := MaxSessionIdentityValueLength
		if source.Type == SessionIdentitySourceGjson {
			maxBytes = maxSessionIdentityGJSONBytes
		}
		if value == "" || len(value) > maxBytes {
			continue
		}
		pattern := cfg.ValueRegex
		if source.ValueRegex != nil {
			pattern = *source.ValueRegex
		}
		if pattern != "" {
			re, ok := compileSessionIdentityRegex(pattern)
			if !ok || re == nil || !re.MatchString(value) {
				continue
			}
		}
		return value, sessionIdentitySourceLabel(source)
	}
	return "", ""
}

// NormalizeSessionIdentityValue applies the configured transform to bound and
// anonymize the identity value.
func NormalizeSessionIdentityValue(value, transform string) string {
	switch transform {
	case SessionIdentityTransformXXHash:
		return fmt.Sprintf("%016x", xxhash.Sum64String(value))
	case SessionIdentityTransformSHA1_16:
		digest := Sha1([]byte(value))
		if len(digest) > 16 {
			return digest[:16]
		}
		return digest
	default:
		return value
	}
}

func truncateSessionIdentityValue(value string) string {
	if len(value) > 64 {
		return value[:64]
	}
	return value
}
