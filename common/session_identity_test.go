package common

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func sessionIdentityTestBody(model string, systemBlocks []string, userContent string) string {
	blocks := make([]string, 0, len(systemBlocks))
	for _, text := range systemBlocks {
		blocks = append(blocks, fmt.Sprintf(`{"type":"text","text":%q}`, text))
	}
	return fmt.Sprintf(`{"model":%q,"system":[%s],"messages":[{"role":"user","content":%q}]}`,
		model, strings.Join(blocks, ","), userContent)
}

// systemIdentityFixture builds the shape used by the customer that motivated
// this feature: a large fixed system prefix, two blocks appended per turn, and
// only the latest user message in messages (so the legacy first-message seed
// would rotate every turn).
func systemIdentityFixture(marker string) []string {
	blocks := make([]string, 0, 48+12)
	for i := range 48 {
		blocks = append(blocks, fmt.Sprintf("[%s-fixed-%d] stable synthetic prefix text", marker, i))
	}
	for turn := 1; turn <= 6; turn++ {
		blocks = append(blocks, fmt.Sprintf("[t%d-0-x%04d] appended turn text", turn, turn*7))
		blocks = append(blocks, fmt.Sprintf("[t%d-1-y%04d] appended turn text", turn, turn*11))
	}
	return blocks
}

func systemIdentityConfig() *SessionIdentityConfig {
	return &SessionIdentityConfig{
		Enabled:       true,
		ModelPrefixes: []string{"claude-"},
		Sources: []SessionIdentitySource{
			{Type: SessionIdentitySourceHeader, Key: "x-session-id"},
			{
				Type:         SessionIdentitySourceRegex,
				Path:         "system.0.text",
				Pattern:      `^\[([^\]]+)-fixed-\d+\]`,
				CaptureGroup: 1,
			},
		},
		Transform:                   SessionIdentityTransformXXHash,
		ValueRegex:                  `^[A-Za-z0-9_-]{4,64}$`,
		OnEmpty:                     SessionIdentityOnEmptyLegacy,
		DisableFirstMessageFallback: true,
	}
}

// TestSessionIdentityStableAcrossTurns is the regression fixture for the
// six-turn system-prefix customer: every turn must resolve to the same session
// id extracted from the fixed first system block.
func TestSessionIdentityStableAcrossTurns(t *testing.T) {
	cfg := systemIdentityConfig()
	blocks := systemIdentityFixture("v54qx6")
	ids := make([]string, 0, 6)
	for turn := 1; turn <= 6; turn++ {
		// Each request includes one more turn pair than the previous one.
		body := sessionIdentityTestBody("claude-opus-4-6", blocks[:48+turn*2], fmt.Sprintf("task %d: reply", turn))
		resolution := ResolveSessionIdentity([]byte(body), "", "10.0.0.1", "test-agent", "test-key", nil, cfg)
		require.Equal(t, "custom:regex(system.0.text)", resolution.Source)
		require.Equal(t, "v54qx6", resolution.RawValue)
		require.Len(t, resolution.SessionID, 16)
		ids = append(ids, resolution.SessionID)
	}
	for _, id := range ids[1:] {
		assert.Equal(t, ids[0], id)
	}
}

func TestSessionIdentityDifferentMarkersAreDifferentSessions(t *testing.T) {
	cfg := systemIdentityConfig()
	first := ResolveSessionIdentity([]byte(sessionIdentityTestBody("claude-x", systemIdentityFixture("aaaaaa"), "hi")), "", "ip", "ua", "key", nil, cfg)
	second := ResolveSessionIdentity([]byte(sessionIdentityTestBody("claude-x", systemIdentityFixture("bbbbbb"), "hi")), "", "ip", "ua", "key", nil, cfg)
	require.NotEmpty(t, first.SessionID)
	assert.NotEqual(t, first.SessionID, second.SessionID)
}

// TestSessionIdentityNonTargetModelSkipsExtraction proves the short-circuit:
// when the customer config does not match the model, the regex must not run
// and the legacy heuristic is used instead.
func TestSessionIdentityNonTargetModelSkipsExtraction(t *testing.T) {
	cfg := systemIdentityConfig()
	cfg.ModelPrefixes = []string{"gpt-"}
	body := sessionIdentityTestBody("claude-opus-4-6", systemIdentityFixture("v54qx6"), "latest turn")
	resolution := ResolveSessionIdentity([]byte(body), "", "10.0.0.1", "ua", "api-key", nil, cfg)
	assert.Equal(t, "legacy:first_message", resolution.Source)
	assert.Empty(t, resolution.RawValue)
	assert.Equal(t, ClaudeSessionHash(DeriveClaudeSessionSeed([]byte(body), "", "10.0.0.1", "ua", "api-key")), resolution.SessionID)
}

func TestSessionIdentityClientMetadataShortCircuit(t *testing.T) {
	cfg := systemIdentityConfig()
	body := `{"model":"claude-x","metadata":{"user_id":"{\"device_id\":\"abc\",\"session_id\":\"s1\"}"},"messages":[]}`
	resolution := ResolveSessionIdentity([]byte(body), "", "ip", "ua", "key", nil, cfg)
	assert.Empty(t, resolution.SessionID)
	assert.Equal(t, "client", resolution.Source)
}

func TestSessionIdentityHeaderWinsOverCustomSources(t *testing.T) {
	cfg := systemIdentityConfig()
	body := sessionIdentityTestBody("claude-x", systemIdentityFixture("v54qx6"), "hi")
	resolution := ResolveSessionIdentity([]byte(body), "header-session", "ip", "ua", "key", nil, cfg)
	assert.Equal(t, "header", resolution.Source)
	assert.Equal(t, "header-session", resolution.RawValue)
	want := ClaudeSessionHash(strings.Join([]string{"claude-x", claudeStableSystemPrefix([]byte(body)), "header-session"}, "\x1f"))
	assert.Equal(t, want, resolution.SessionID)
}

func TestSessionIdentityHeaderSource(t *testing.T) {
	cfg := systemIdentityConfig()
	body := sessionIdentityTestBody("claude-x", systemIdentityFixture("v54qx6"), "hi")
	headers := http.Header{}
	headers.Set("x-session-id", "custom-header-session")
	resolution := ResolveSessionIdentity([]byte(body), "", "ip", "ua", "key", headers, cfg)
	assert.Equal(t, "custom:request_header(x-session-id)", resolution.Source)
	assert.Equal(t, "custom-header-session", resolution.RawValue)
}

func TestExtractSessionIdentitySources(t *testing.T) {
	t.Run("metadata user id with inner json string field", func(t *testing.T) {
		body := `{"metadata":{"user_id":"{\"device_id\":\"abc123\",\"session_id\":\"sess-1\"}"}}`
		value, source := ExtractSessionIdentity(&SessionIdentityConfig{
			Sources: []SessionIdentitySource{{
				Type:            SessionIdentitySourceClientMetadata,
				JSONStringField: "session_id",
			}},
		}, []byte(body), nil)
		assert.Equal(t, "sess-1", value)
		assert.Equal(t, "metadata_user_id", source)
	})

	t.Run("gjson path", func(t *testing.T) {
		body := `{"a":{"b":" value "},"c":42}`
		value, source := ExtractSessionIdentity(&SessionIdentityConfig{
			Sources: []SessionIdentitySource{{Type: SessionIdentitySourceGjson, Path: "a.b"}},
		}, []byte(body), nil)
		assert.Equal(t, "value", value)
		assert.Equal(t, "gjson(a.b)", source)
	})

	t.Run("regex capture group", func(t *testing.T) {
		body := `{"system":[{"type":"text","text":"[marker-fixed-0] hello"}]}`
		value, source := ExtractSessionIdentity(&SessionIdentityConfig{
			Sources: []SessionIdentitySource{{
				Type:         SessionIdentitySourceRegex,
				Path:         "system.0.text",
				Pattern:      `^\[([^\]]+)-fixed-\d+\]`,
				CaptureGroup: 1,
			}},
		}, []byte(body), nil)
		assert.Equal(t, "marker", value)
		assert.Equal(t, "regex(system.0.text)", source)
	})

	t.Run("invalid regex and missing capture groups do not match", func(t *testing.T) {
		body := `{"system":[{"type":"text","text":"[marker-fixed-0]"}]}`
		for _, source := range []SessionIdentitySource{
			{Type: SessionIdentitySourceRegex, Path: "system.0.text", Pattern: "("},
			{Type: SessionIdentitySourceRegex, Path: "system.0.text", Pattern: `^\[(.+)-fixed-\d+\]`, CaptureGroup: 5},
			{Type: SessionIdentitySourceRegex, Path: "system.1.text", Pattern: `^\[(.+)\]`},
		} {
			value, _ := ExtractSessionIdentity(&SessionIdentityConfig{Sources: []SessionIdentitySource{source}}, []byte(body), nil)
			assert.Empty(t, value)
		}
	})

	t.Run("value regex rejects and falls through to the next source", func(t *testing.T) {
		body := `{"system":[{"type":"text","text":"[marker-fixed-0]"}],"messages":[{"role":"user","content":"sess-real"}]}`
		cfg := &SessionIdentityConfig{
			ValueRegex: `^sess-`,
			Sources: []SessionIdentitySource{
				{Type: SessionIdentitySourceRegex, Path: "system.0.text", Pattern: `^\[(.+)-fixed-\d+\]`, CaptureGroup: 1},
				{Type: SessionIdentitySourceGjson, Path: "messages.0.content"},
			},
		}
		value, source := ExtractSessionIdentity(cfg, []byte(body), nil)
		assert.Equal(t, "sess-real", value)
		assert.Equal(t, "gjson(messages.0.content)", source)
	})

	t.Run("oversized values are rejected", func(t *testing.T) {
		long := strings.Repeat("a", MaxSessionIdentityValueLength+1)
		body := fmt.Sprintf(`{"a":%q}`, long)
		value, _ := ExtractSessionIdentity(&SessionIdentityConfig{
			Sources: []SessionIdentitySource{{Type: SessionIdentitySourceGjson, Path: "a"}},
		}, []byte(body), nil)
		assert.Empty(t, value)
	})
}

func TestSessionIdentityOnEmptyPolicies(t *testing.T) {
	body := `{"model":"claude-x","messages":[{"role":"user","content":"first"}]}`

	t.Run("legacy default uses first message", func(t *testing.T) {
		resolution := ResolveSessionIdentity([]byte(body), "", "ip", "ua", "key", nil,
			&SessionIdentityConfig{Enabled: true})
		assert.Equal(t, "legacy:first_message", resolution.Source)
	})

	t.Run("disable first message fallback falls to fingerprint", func(t *testing.T) {
		resolution := ResolveSessionIdentity([]byte(body), "", "ip", "ua", "key", nil,
			&SessionIdentityConfig{Enabled: true, DisableFirstMessageFallback: true})
		assert.Equal(t, "legacy:client_fingerprint", resolution.Source)
		want := ClaudeSessionHash(strings.Join([]string{
			"claude-x", claudeStableSystemPrefix([]byte(body)), "ip" + "ua" + "key",
		}, "\x1f"))
		assert.Equal(t, want, resolution.SessionID)
	})

	t.Run("stable skips first message", func(t *testing.T) {
		resolution := ResolveSessionIdentity([]byte(body), "", "ip", "ua", "key", nil,
			&SessionIdentityConfig{Enabled: true, OnEmpty: SessionIdentityOnEmptyStable})
		assert.Equal(t, "legacy:client_fingerprint", resolution.Source)
	})

	t.Run("none injects nothing", func(t *testing.T) {
		resolution := ResolveSessionIdentity([]byte(body), "", "ip", "ua", "key", nil,
			&SessionIdentityConfig{Enabled: true, OnEmpty: SessionIdentityOnEmptyNone})
		assert.Equal(t, "none", resolution.Source)
		assert.Empty(t, resolution.SessionID)
	})
}

func TestSessionIdentityPrepareEmbedsCustomSession(t *testing.T) {
	cfg := systemIdentityConfig()
	body := sessionIdentityTestBody("claude-opus-4-6", systemIdentityFixture("v54qx6"), "hi")
	out, synthesized, err := PrepareClaudeMetadataUserIDWithConfig([]byte(body), "secret-key-XYZ789", "", "ip", "ua", nil, cfg)
	require.NoError(t, err)
	require.True(t, synthesized)

	blob := gjson.Get(string(out), "metadata.user_id").String()
	assert.NotEmpty(t, blob)
	inner := gjson.Get(blob, "session_id").String()
	resolution := ResolveSessionIdentity([]byte(body), "", "ip", "ua", "secret-key-XYZ789", nil, cfg)
	assert.Equal(t, resolution.SessionID, inner)
	assert.Equal(t, "XYZ789", gjson.Get(blob, "device_id").String())

	// A client-provided user_id is still preserved verbatim.
	clientBody := `{"model":"claude-x","metadata":{"user_id":"{\"session_id\":\"keep\"}"}}`
	out, synthesized, err = PrepareClaudeMetadataUserIDWithConfig([]byte(clientBody), "secret-key-XYZ789", "", "ip", "ua", nil, cfg)
	require.NoError(t, err)
	assert.False(t, synthesized)
	assert.Equal(t, clientBody, string(out))
}

func TestSessionIdentityConfigClone(t *testing.T) {
	cfg := &SessionIdentityConfig{
		Enabled:       true,
		ModelPrefixes: []string{" CLAUDE-", "claude-", ""},
		Sources: []SessionIdentitySource{{
			Type:    SessionIdentitySourceHeader,
			Key:     "X-Session-Id",
			Pattern: "p",
		}},
	}
	clone := cfg.Clone()
	require.NotNil(t, clone)
	assert.Equal(t, []string{"CLAUDE-"}, clone.ModelPrefixes)

	clone.ModelPrefixes[0] = "changed"
	clone.Sources[0].Key = "changed"
	assert.Equal(t, []string{" CLAUDE-", "claude-", ""}, cfg.ModelPrefixes)
	assert.Equal(t, "X-Session-Id", cfg.Sources[0].Key)

	assert.Nil(t, (*SessionIdentityConfig)(nil).Clone())
	assert.False(t, (*SessionIdentityConfig)(nil).Matches("claude-x"))
	assert.False(t, (&SessionIdentityConfig{}).Matches("claude-x"))
	assert.True(t, (&SessionIdentityConfig{Enabled: true}).Matches("claude-x"))
}
