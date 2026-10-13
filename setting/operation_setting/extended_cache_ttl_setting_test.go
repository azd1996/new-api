package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldForceExtendedCacheTTL(t *testing.T) {
	// Explicitly initialize the setting state for this test.
	extendedCacheTTLSetting = ExtendedCacheTTLSetting{
		Rules: []ExtendedCacheTTLRule{
			{Group: "groupA", Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"}},
			{Group: "groupB", Models: []string{"claude-*"}},
			{Group: "groupC", Models: []string{"*"}},
		},
	}
	t.Cleanup(func() { extendedCacheTTLSetting = ExtendedCacheTTLSetting{} })

	cases := []struct {
		name  string
		group string
		model string
		want  bool
	}{
		{"exact match in group", "groupA", "claude-opus-5-5", true},
		{"exact model not listed", "groupA", "claude-haiku-5-5", false},
		{"prefix match", "groupB", "claude-haiku-5-5", true},
		{"prefix non-match", "groupB", "gpt-5", false},
		{"wildcard matches any model", "groupC", "whatever-model", true},
		{"group not configured", "groupX", "claude-opus-5-5", false},
		{"empty group", "", "claude-opus-5-5", false},
		{"empty model", "groupA", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ShouldForceExtendedCacheTTL(tc.group, tc.model))
		})
	}
}
