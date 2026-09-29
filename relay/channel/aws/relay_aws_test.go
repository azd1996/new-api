package aws

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFormatRequestPreservesThinkingBlockBinding(t *testing.T) {
	requestBody := bytes.NewBufferString(`{
		"messages": [{"role": "user", "content": "hello"}],
		"max_tokens": 1024,
		"thinking": {
			"type": "adaptive",
			"block_binding": {
				"future_field": {"enabled": true}
			}
		}
	}`)

	request, err := formatRequest(requestBody, http.Header{})
	require.NoError(t, err)
	require.NotNil(t, request.Thinking)
	require.JSONEq(t, `{"future_field":{"enabled":true}}`, string(request.Thinking.BlockBinding))

	marshaled, err := common.Marshal(request)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, common.Unmarshal(marshaled, &got))
	require.Equal(t, map[string]any{"enabled": true}, got["thinking"].(map[string]any)["block_binding"].(map[string]any)["future_field"])
}

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}
