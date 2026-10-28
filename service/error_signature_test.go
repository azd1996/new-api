package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeErrorSignatureBedrockExample locks the canonical transformation
// the feature is built around: the trailing new-api `(request id: ...)` suffixes
// are stripped and the embedded AWS RequestID UUID is masked while the rest of
// the message (including the `status_code=400` prefix and the `content.0` index)
// is preserved verbatim.
func TestNormalizeErrorSignatureBedrockExample(t *testing.T) {
	require.NoError(t, SetErrorSignatureRules(DefaultErrorSignatureRules()))

	input := "status_code=400, InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: 31e9df3c-9b25-4c41-87b5-f488925da305, ValidationException: ***.***.content.0: Invalid `signature` in `thinking` block (request id: 20261022064033677950145A81UZSgs) (request id: 202610220640310817494408268d9d6tsVahoWk) (request id: 67f75edb3cdb3d290c2052b111f99051)"
	want := "status_code=400, InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: ***.***.content.0: Invalid `signature` in `thinking` block"

	assert.Equal(t, want, NormalizeErrorSignature(input))
}

func TestNormalizeErrorSignatureRules(t *testing.T) {
	require.NoError(t, SetErrorSignatureRules(DefaultErrorSignatureRules()))

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "strips multiple newapi suffixes",
			input: "upstream exploded (request id: abc) (request id: def)",
			want:  "upstream exploded",
		},
		{
			name:  "masks non-uuid request id by key",
			input: "status_code=500, RequestId: AB12cd34EF56 something",
			want:  "status_code=500, RequestId: * something",
		},
		{
			name:  "masks long hex token",
			input: "trace 67f75edb3cdb3d290c2052b111f99051 failed",
			want:  "trace * failed",
		},
		{
			name:  "masks openai responses item id",
			input: "status_code=400, reasoning item rs_051dff7379a0ef60016adb2a53ca948194a8282bff7c2d471e was not found.",
			want:  "status_code=400, reasoning item rs_* was not found.",
		},
		{
			name:  "masks anthropic bedrock tool id",
			input: "status_code=400, unexpected `tool_use_id`: toolu_bdrk_01QNt6oBGUi8AtDkLkJtT4CN. Each block must match.",
			want:  "status_code=400, unexpected `tool_use_id`: toolu_*. Each block must match.",
		},
		{
			name:  "masks encrypted content blob",
			input: "status_code=400, The encrypted content gAAA...Mw== could not be verified. Reason: Encrypted content could not be decrypted or parsed.",
			want:  "status_code=400, The encrypted content * could not be verified. Reason: Encrypted content could not be decrypted or parsed.",
		},
		{
			name:  "encrypted content body collapses regardless of inner shape",
			input: "status_code=400, The encrypted content for item rs_051dff7379a0ef60016adb2a could not be verified.",
			want:  "status_code=400, The encrypted content * could not be verified.",
		},
		{
			name:  "collapses whitespace and newlines",
			input: "line one\n   line two\tend",
			want:  "line one line two end",
		},
		{
			name:  "masks iso timestamp",
			input: "failed at 2026-10-22T06:40:33Z retry later",
			want:  "failed at * retry later",
		},
		{
			name:  "keeps structural small numbers",
			input: "status_code=400, content.0 invalid, limit 400",
			want:  "status_code=400, content.0 invalid, limit 400",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NormalizeErrorSignature(tc.input))
		})
	}
}

// TestNormalizeErrorSignatureStability guarantees two errors differing only by
// request ids collapse to the same signature (and thus the same hash), while a
// genuinely different error does not.
func TestNormalizeErrorSignatureStability(t *testing.T) {
	require.NoError(t, SetErrorSignatureRules(DefaultErrorSignatureRules()))

	a := NormalizeErrorSignature("boom (request id: 111aaa)")
	b := NormalizeErrorSignature("boom (request id: 222bbb)")
	c := NormalizeErrorSignature("different boom (request id: 333ccc)")

	assert.Equal(t, a, b)
	assert.Equal(t, ErrorSignatureHash(a), ErrorSignatureHash(b))
	assert.NotEqual(t, a, c)
	assert.NotEqual(t, ErrorSignatureHash(a), ErrorSignatureHash(c))
	assert.Len(t, ErrorSignatureHash(a), 32)
	assert.Equal(t, "", ErrorSignatureHash(""))
}

// TestSetErrorSignatureRulesRejectsBadPattern verifies an invalid regexp is
// rejected and leaves the previously-installed rules untouched.
func TestSetErrorSignatureRulesRejectsBadPattern(t *testing.T) {
	require.NoError(t, SetErrorSignatureRules(DefaultErrorSignatureRules()))
	before := NormalizeErrorSignature("boom (request id: x)")

	err := SetErrorSignatureRules([]SignatureRule{{Name: "bad", Pattern: "(", Replacement: "", Enabled: true}})
	require.Error(t, err)

	assert.Equal(t, before, NormalizeErrorSignature("boom (request id: x)"))
}

// TestApplyErrorSignatureRulesJSON covers the persisted-config round trip: a
// valid JSON rule set applies and is readable back; empty input is a no-op that
// keeps the current rules; invalid JSON is rejected without changing anything.
func TestApplyErrorSignatureRulesJSON(t *testing.T) {
	require.NoError(t, SetErrorSignatureRules(DefaultErrorSignatureRules()))

	custom := `[{"name":"mask-foo","pattern":"foo[0-9]+","replacement":"foo*","enabled":true}]`
	require.NoError(t, ApplyErrorSignatureRulesJSON(custom))
	assert.Equal(t, "bar foo* baz", NormalizeErrorSignature("bar foo12345 baz"))

	rules := GetEffectiveErrorSignatureRules()
	require.Len(t, rules, 1)
	assert.Equal(t, "mask-foo", rules[0].Name)

	// Empty string is a no-op: the custom rule stays in effect.
	require.NoError(t, ApplyErrorSignatureRulesJSON(""))
	assert.Len(t, GetEffectiveErrorSignatureRules(), 1)

	// Invalid JSON is rejected and leaves the current rules untouched.
	require.Error(t, ApplyErrorSignatureRulesJSON("not json"))
	assert.Len(t, GetEffectiveErrorSignatureRules(), 1)

	require.NoError(t, SetErrorSignatureRules(DefaultErrorSignatureRules()))
}
