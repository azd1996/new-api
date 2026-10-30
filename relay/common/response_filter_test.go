package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestFilterResponseJSON_RecursiveKeyDelete(t *testing.T) {
	body := []byte(`{
		"id": "chatcmpl-1",
		"prompt_filter_results": [{"prompt_index": 0, "content_filter_results": {"hate": {"filtered": false}}}],
		"choices": [
			{
				"index": 0,
				"finish_reason": "stop",
				"content_filter_results": {"violence": {"filtered": false, "severity": "safe"}},
				"message": {"role": "assistant", "content": "hello"}
			},
			{
				"index": 1,
				"content_filter_results": {"sexual": {"filtered": true}},
				"message": {"role": "assistant", "content": "world"}
			}
		],
		"usage": {"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}
	}`)

	out := FilterResponseJSON(body, []string{"content_filter_results", "prompt_filter_results"})

	require.True(t, gjson.ValidBytes(out))
	assert.False(t, gjson.GetBytes(out, "prompt_filter_results").Exists(), "top-level prompt_filter_results must be removed")
	assert.False(t, gjson.GetBytes(out, "choices.0.content_filter_results").Exists(), "per-choice content_filter_results must be removed")
	assert.False(t, gjson.GetBytes(out, "choices.1.content_filter_results").Exists(), "per-choice content_filter_results must be removed")

	// unrelated fields are preserved
	assert.Equal(t, "hello", gjson.GetBytes(out, "choices.0.message.content").String())
	assert.Equal(t, "world", gjson.GetBytes(out, "choices.1.message.content").String())
	assert.Equal(t, "stop", gjson.GetBytes(out, "choices.0.finish_reason").String())
	assert.Equal(t, int64(5), gjson.GetBytes(out, "usage.total_tokens").Int())
	assert.Equal(t, "chatcmpl-1", gjson.GetBytes(out, "id").String())
}

func TestFilterResponseJSON_RecursiveKeyDeleteResponsesArray(t *testing.T) {
	// Azure Responses API attaches content_filters as a nested array on the
	// response object; recursive key deletion must reach it at any depth.
	body := []byte(`{
		"id": "resp_1",
		"object": "response",
		"content_filters": [
			{"source_type": "prompt", "content_filter_offsets": {"start_offset": 0}, "content_filter_results": {"hate": {"filtered": false}}}
		],
		"output": [{"type": "message", "content": [{"type": "output_text", "text": "ok"}]}]
	}`)

	out := FilterResponseJSON(body, []string{"content_filters"})

	require.True(t, gjson.ValidBytes(out))
	assert.False(t, gjson.GetBytes(out, "content_filters").Exists())
	assert.Equal(t, "ok", gjson.GetBytes(out, "output.0.content.0.text").String())
	assert.Equal(t, "resp_1", gjson.GetBytes(out, "id").String())
}

func TestFilterResponseJSON_ExactPathDelete(t *testing.T) {
	body := []byte(`{"choices":[{"content_filter_results":{"a":1},"text":"x"},{"content_filter_results":{"b":2},"text":"y"}]}`)

	out := FilterResponseJSON(body, []string{"choices.0.content_filter_results"})

	require.True(t, gjson.ValidBytes(out))
	assert.False(t, gjson.GetBytes(out, "choices.0.content_filter_results").Exists(), "exact-path target removed")
	assert.True(t, gjson.GetBytes(out, "choices.1.content_filter_results").Exists(), "sibling must be preserved by exact path")
	assert.Equal(t, "x", gjson.GetBytes(out, "choices.0.text").String())
}

func TestFilterResponseJSON_SimilarKeyNotDeleted(t *testing.T) {
	// Key deletion is exact-match: a differently named field must survive.
	body := []byte(`{"content_filter_results":{"x":1},"content_filter_results_extra":{"y":2}}`)

	out := FilterResponseJSON(body, []string{"content_filter_results"})

	require.True(t, gjson.ValidBytes(out))
	assert.False(t, gjson.GetBytes(out, "content_filter_results").Exists())
	assert.True(t, gjson.GetBytes(out, "content_filter_results_extra").Exists())
}

func TestFilterResponseJSON_NoOpCases(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		rules []string
	}{
		{name: "empty rules", body: `{"content_filter_results":{"a":1}}`, rules: nil},
		{name: "blank rules only", body: `{"content_filter_results":{"a":1}}`, rules: []string{"", "   "}},
		{name: "non-json done sentinel", body: `[DONE]`, rules: []string{"content_filter_results"}},
		{name: "non-json plain text", body: `not json at all`, rules: []string{"content_filter_results"}},
		{name: "empty body", body: ``, rules: []string{"content_filter_results"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := FilterResponseJSON([]byte(tc.body), tc.rules)
			assert.Equal(t, tc.body, string(out), "body must be returned unchanged")
		})
	}
}
