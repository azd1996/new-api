package dto_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeThinkingPreservesBlockBinding(t *testing.T) {
	tests := []struct {
		name         string
		bindingField string
		want         string
	}{
		{name: "absent"},
		{name: "empty object", bindingField: `,"block_binding":{}`, want: `{}`},
		{name: "null", bindingField: `,"block_binding":null`, want: `null`},
		{name: "other fields only", bindingField: `,"block_binding":{"future_field":{"enabled":true}}`, want: `{"future_field":{"enabled":true}}`},
		{name: "mixed fields", bindingField: `,"block_binding":{"prefix_mismatch_behavior":"drop_block","future_field":42}`, want: `{"prefix_mismatch_behavior":"drop_block","future_field":42}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestJSON := fmt.Sprintf(`{"model":"claude-opus-4-8","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"adaptive"%s}}`, tt.bindingField)
			var request dto.ClaudeRequest
			require.NoError(t, common.Unmarshal([]byte(requestJSON), &request))
			require.NotNil(t, request.Thinking)

			marshaled, err := common.Marshal(&request)
			require.NoError(t, err)
			var result struct {
				Thinking map[string]json.RawMessage `json:"thinking"`
			}
			require.NoError(t, common.Unmarshal(marshaled, &result))
			binding, exists := result.Thinking["block_binding"]
			if tt.want == "" {
				assert.False(t, exists)
				return
			}
			require.True(t, exists)
			require.JSONEq(t, tt.want, string(binding))
		})
	}
}
