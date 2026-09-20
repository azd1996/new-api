package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestIsChatRolePreamble(t *testing.T) {
	tests := []struct {
		name string
		resp *dto.ChatCompletionsStreamResponse
		want bool
	}{
		{
			name: "role only preamble",
			resp: &dto.ChatCompletionsStreamResponse{
				Choices: []dto.ChatCompletionsStreamResponseChoice{
					{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}},
				},
			},
			want: true,
		},
		{
			name: "role with content is not a preamble",
			resp: &dto.ChatCompletionsStreamResponse{
				Choices: []dto.ChatCompletionsStreamResponseChoice{
					{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant", Content: strPtr("hi")}},
				},
			},
			want: false,
		},
		{
			name: "content only delta is not a preamble",
			resp: &dto.ChatCompletionsStreamResponse{
				Choices: []dto.ChatCompletionsStreamResponseChoice{
					{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: strPtr("hi")}},
				},
			},
			want: false,
		},
		{
			name: "role delta with finish reason is not a preamble",
			resp: &dto.ChatCompletionsStreamResponse{
				Choices: []dto.ChatCompletionsStreamResponseChoice{
					{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}, FinishReason: strPtr("stop")},
				},
			},
			want: false,
		},
		{
			name: "no choices",
			resp: &dto.ChatCompletionsStreamResponse{},
			want: false,
		},
		{
			name: "nil response",
			resp: nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isChatRolePreamble(tt.resp))
		})
	}
}

// guardInfoWithRule builds a minimal RelayInfo whose retry_override matches any
// response body containing "boom", so the pre-commit guard can be exercised.
func guardInfoWithRule() *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
	}
	info.ChannelSetting = dto.ChannelSettings{
		RetryOverride: []dto.RetryRule{
			{
				Phase2ResponseConditions: &dto.RetryRulePhaseCondition{
					Conditions: []map[string]any{
						{"path": "response_body", "mode": "contains", "value": "boom"},
					},
				},
			},
		},
	}
	return info
}

func TestPrecommitChunksDefault(t *testing.T) {
	assert.Equal(t, retryOverridePrecommitMaxChunks, precommitChunks(nil))

	// ChannelSetting is promoted from the embedded *ChannelMeta, so a non-nil
	// ChannelMeta is required to read/set it.
	unset := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	assert.Equal(t, retryOverridePrecommitMaxChunks, precommitChunks(unset))

	negative := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	negative.ChannelSetting = dto.ChannelSettings{RetryOverrideStreamPrecommitChunks: -1}
	assert.Equal(t, retryOverridePrecommitMaxChunks, precommitChunks(negative))

	custom := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	custom.ChannelSetting = dto.ChannelSettings{RetryOverrideStreamPrecommitChunks: 5}
	assert.Equal(t, 5, precommitChunks(custom))
}

func TestPrecommitGuardMatchInWindowForwardsNothing(t *testing.T) {
	g := newPrecommitGuard(guardInfoWithRule(), 200)
	var forwarded []string
	process := func(d string) { forwarded = append(forwarded, d) }

	// Preamble chunk: no match, held in the buffer, nothing forwarded yet.
	require.False(t, g.feed(`{"type":"response.created"}`, false, process))
	assert.Empty(t, forwarded)
	assert.False(t, g.committed)

	// Error chunk matches the rule: guard signals stop, still nothing forwarded.
	require.True(t, g.feed(`{"type":"error","message":"boom rate limit"}`, false, process))
	assert.Empty(t, forwarded)
	assert.NotNil(t, g.matched)
	assert.False(t, g.committed)
}

func TestPrecommitGuardCommitsOnContentInOrder(t *testing.T) {
	g := newPrecommitGuard(guardInfoWithRule(), 200)
	var forwarded []string
	process := func(d string) { forwarded = append(forwarded, d) }

	require.False(t, g.feed(`{"type":"response.created"}`, false, process))
	assert.Empty(t, forwarded)

	// First content chunk commits: buffered preamble + this chunk flush in order.
	require.False(t, g.feed(`{"type":"response.output_text.delta","delta":"hi"}`, true, process))
	assert.Equal(t, []string{`{"type":"response.created"}`, `{"type":"response.output_text.delta","delta":"hi"}`}, forwarded)
	assert.True(t, g.committed)
	assert.Nil(t, g.matched)

	// Post-commit chunks pass straight through, even if they would have matched.
	require.False(t, g.feed(`{"type":"error","message":"boom"}`, false, process))
	assert.Equal(t, 3, len(forwarded))
	assert.Nil(t, g.matched)
}

func TestPrecommitGuardChunkCapForcesCommit(t *testing.T) {
	g := newPrecommitGuard(guardInfoWithRule(), 200) // default cap = 3
	var forwarded []string
	process := func(d string) { forwarded = append(forwarded, d) }

	require.False(t, g.feed(`{"type":"response.created"}`, false, process))
	require.False(t, g.feed(`{"type":"response.in_progress"}`, false, process))
	assert.Empty(t, forwarded)
	// Third preamble hits the cap and forces a commit even without a content chunk.
	require.False(t, g.feed(`{"type":"response.queued"}`, false, process))
	assert.Equal(t, 3, len(forwarded))
	assert.True(t, g.committed)
}

func TestPrecommitGuardNoRulesPassthrough(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	g := newPrecommitGuard(info, 200)
	require.True(t, g.committed) // no retry_override → no buffering

	var forwarded []string
	process := func(d string) { forwarded = append(forwarded, d) }
	// A body that would match if scanned is passed through untouched.
	require.False(t, g.feed(`{"type":"error","message":"boom"}`, false, process))
	assert.Equal(t, []string{`{"type":"error","message":"boom"}`}, forwarded)
	assert.Nil(t, g.matched)
}

func TestPrecommitGuardFinishFlushesBuffer(t *testing.T) {
	g := newPrecommitGuard(guardInfoWithRule(), 200)
	var forwarded []string
	process := func(d string) { forwarded = append(forwarded, d) }

	require.False(t, g.feed(`{"type":"response.created"}`, false, process))
	assert.Empty(t, forwarded)
	// Stream ended inside the window without matching → finish flushes.
	g.finish(process)
	assert.Equal(t, []string{`{"type":"response.created"}`}, forwarded)
	assert.True(t, g.committed)
}
