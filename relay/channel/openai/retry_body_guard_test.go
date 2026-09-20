package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
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
