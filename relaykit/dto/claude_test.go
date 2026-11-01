package dto

import (
	"strings"
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Some non-compliant upstreams return a bare string for the top-level "message"
// field of a Claude response. Strict unmarshaling used to fail with
// `cannot unmarshal string into ... ClaudeMediaMessage` and surface as a 500.
// The custom ClaudeMediaMessage.UnmarshalJSON must tolerate the string while
// still parsing well-formed message objects unchanged.
func TestClaudeResponseMessageStringTolerated(t *testing.T) {
	t.Run("string message does not fail parsing", func(t *testing.T) {
		var resp ClaudeResponse
		err := kitutil.UnmarshalJsonStr(`{"type":"message_start","message":"upstream raw text"}`, &resp)
		require.NoError(t, err)
		require.NotNil(t, resp.Message)
		assert.Equal(t, "upstream raw text", resp.Message.GetText())
	})

	t.Run("object message still parses fields", func(t *testing.T) {
		var resp ClaudeResponse
		err := kitutil.UnmarshalJsonStr(`{"type":"message_start","message":{"id":"msg_1","model":"claude-3-5-sonnet","usage":{"input_tokens":11,"output_tokens":22}}}`, &resp)
		require.NoError(t, err)
		require.NotNil(t, resp.Message)
		assert.Equal(t, "msg_1", resp.Message.Id)
		assert.Equal(t, "claude-3-5-sonnet", resp.Message.Model)
		require.NotNil(t, resp.Message.Usage)
		assert.Equal(t, 11, resp.Message.Usage.InputTokens)
		assert.Equal(t, 22, resp.Message.Usage.OutputTokens)
	})

	t.Run("content array of objects still parses", func(t *testing.T) {
		var resp ClaudeResponse
		err := kitutil.UnmarshalJsonStr(`{"type":"message","content":[{"type":"text","text":"hello"}]}`, &resp)
		require.NoError(t, err)
		require.Len(t, resp.Content, 1)
		assert.Equal(t, "text", resp.Content[0].Type)
		assert.Equal(t, "hello", resp.Content[0].GetText())
	})

	t.Run("content array with a bare string element is tolerated", func(t *testing.T) {
		var resp ClaudeResponse
		err := kitutil.UnmarshalJsonStr(`{"type":"message","content":["stray"]}`, &resp)
		require.NoError(t, err)
		require.Len(t, resp.Content, 1)
		assert.Equal(t, "stray", resp.Content[0].GetText())
	})
}

// A mid-conversation system message carries clear_at ("next_user_message" or
// "never"); dropping it on re-marshal turns a turn-scoped reminder into a
// permanent one upstream.
func TestClaudeRequestKeepsMessageClearAt(t *testing.T) {
	var req ClaudeRequest
	require.NoError(t, kitutil.UnmarshalJsonStr(`{"model":"m","messages":[
		{"role":"user","content":"hi"},
		{"role":"system","clear_at":"next_user_message","content":"<total_tokens>9 tokens left</total_tokens>"}
	]}`, &req))

	out, err := kitutil.Marshal(req)
	require.NoError(t, err)
	assert.Equal(t, "", req.Messages[0].ClearAt)
	assert.Equal(t, "next_user_message", req.Messages[1].ClearAt)
	assert.Contains(t, string(out), `"clear_at":"next_user_message"`)
	assert.Equal(t, 1, strings.Count(string(out), "clear_at"), "messages without clear_at stay unchanged")
}
