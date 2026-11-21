package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseLLMRuleJSON locks down the robustness of the AI rule parser: models
// are told to return bare JSON but often wrap it in markdown fences or prose,
// and an uncompilable RE2 pattern must be rejected before it reaches the rule
// editor.
func TestParseLLMRuleJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    service.SignatureRule
		wantErr bool
	}{
		{
			name:    "bare JSON with escapes",
			content: `{"name":"url-blocked-by-rules","pattern":"Cannot fetch content from the provided URL\\.","signature":"Cannot fetch content from the provided URL","attribution":"源站问题","note":"blocked URL","enabled":false}`,
			want: service.SignatureRule{
				Name:        "url-blocked-by-rules",
				Pattern:     `Cannot fetch content from the provided URL\.`,
				Signature:   "Cannot fetch content from the provided URL",
				Attribution: "源站问题",
				Note:        "blocked URL",
				Enabled:     true,
			},
		},
		{
			name: "markdown fenced JSON",
			content: "```json\n" +
				`{"name":"invalid-thinking-signature","pattern":"invalid .* signature in request","signature":"invalid thinking signature","attribution":"客户端问题-可解决","note":"","enabled":true}` +
				"\n```",
			want: service.SignatureRule{
				Name:        "invalid-thinking-signature",
				Pattern:     "invalid .* signature in request",
				Signature:   "invalid thinking signature",
				Attribution: "客户端问题-可解决",
				Enabled:     true,
			},
		},
		{
			name: "prose surrounding JSON",
			content: `Here is the rule you asked for:
{"name":"rate-limited","pattern":"try again in [0-9.]+ ?ms","signature":"rate limited","attribution":"上游网关问题","note":"","enabled":true}
Hope this helps!`,
			want: service.SignatureRule{
				Name:        "rate-limited",
				Pattern:     "try again in [0-9.]+ ?ms",
				Signature:   "rate limited",
				Attribution: "上游网关问题",
				Enabled:     true,
			},
		},
		{
			name:    "lookahead is not RE2",
			content: `{"name":"x","pattern":"(?=foo)bar","signature":"s","attribution":"","note":"","enabled":true}`,
			wantErr: true,
		},
		{
			name:    "missing name",
			content: `{"name":"","pattern":"foo","signature":"s","attribution":"","note":"","enabled":true}`,
			wantErr: true,
		},
		{
			name:    "missing pattern",
			content: `{"name":"x","pattern":"","signature":"s","attribution":"","note":"","enabled":true}`,
			wantErr: true,
		},
		{
			name:    "missing signature",
			content: `{"name":"x","pattern":"foo","signature":"","attribution":"","note":"","enabled":true}`,
			wantErr: true,
		},
		{
			name:    "not JSON",
			content: "I could not generate a rule.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, err := parseLLMRuleJSON(tt.content)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, rule)
		})
	}
}

// TestBuildAIRulePrompt verifies the prompt carries the contract and the data
// the model needs: rule semantics, the normalized signature, raw samples, the
// admin's attribution vocabulary, and the existing rule names to avoid.
func TestBuildAIRulePrompt(t *testing.T) {
	prompt := buildAIRulePrompt(
		aiGeneratedRuleRequest{
			Signature:          "ValidationException: invalid signature",
			AttributionOptions: []string{"客户端问题-可解决", "源站问题"},
		},
		[]string{"status_code=400, ValidationException: invalid signature in request 123"},
		[]string{"already-configured-rule"},
	)

	for _, want := range []string{
		"ValidationException: invalid signature",
		"status_code=400, ValidationException: invalid signature in request 123",
		"客户端问题-可解决, 源站问题",
		"already-configured-rule",
		"RE2",
		"kebab-case",
	} {
		assert.Contains(t, prompt, want)
	}
}
