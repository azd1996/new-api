package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestChannelAffinityCoversClaudeModels covers the self-check's core
// decision: whether an enabled channel-affinity rule would route Claude
// model traffic to a sticky upstream channel/key. Cache-optimization hit
// rates depend on this holding true.
func TestChannelAffinityCoversClaudeModels(t *testing.T) {
	previous := channelAffinitySetting
	t.Cleanup(func() { channelAffinitySetting = previous })

	t.Run("disabled affinity never covers Claude", func(t *testing.T) {
		channelAffinitySetting = ChannelAffinitySetting{
			Enabled: false,
			Rules:   []ChannelAffinityRule{{ModelRegex: []string{"^claude-.*$"}}},
		}
		assert.False(t, channelAffinityCoversClaudeModels())
	})

	t.Run("enabled with a Claude-matching rule covers Claude", func(t *testing.T) {
		channelAffinitySetting = ChannelAffinitySetting{
			Enabled: true,
			Rules:   []ChannelAffinityRule{{ModelRegex: []string{"^claude-.*$"}}},
		}
		assert.True(t, channelAffinityCoversClaudeModels())
	})

	t.Run("enabled but only non-Claude rules do not cover Claude", func(t *testing.T) {
		channelAffinitySetting = ChannelAffinitySetting{
			Enabled: true,
			Rules:   []ChannelAffinityRule{{ModelRegex: []string{"^gpt-.*$"}}},
		}
		assert.False(t, channelAffinityCoversClaudeModels())
	})

	t.Run("enabled with no rules does not cover Claude", func(t *testing.T) {
		channelAffinitySetting = ChannelAffinitySetting{Enabled: true}
		assert.False(t, channelAffinityCoversClaudeModels())
	})

	t.Run("an invalid regex is skipped rather than panicking", func(t *testing.T) {
		channelAffinitySetting = ChannelAffinitySetting{
			Enabled: true,
			Rules: []ChannelAffinityRule{
				{ModelRegex: []string{"("}},
				{ModelRegex: []string{"^claude-.*$"}},
			},
		}
		assert.True(t, channelAffinityCoversClaudeModels())
	})
}

func TestClaudeCacheOptimizationGloballyConfigured(t *testing.T) {
	reset := func() {
		extendedCacheTTLSetting = GroupModelRuleSetting{}
		cacheBreakpointFixSetting = GroupModelRuleSetting{}
		stableBaseBreakpointSetting = GroupModelRuleSetting{}
		fallbackCacheBreakpointSetting = GroupModelRuleSetting{}
		retainPreviousBreakpointSetting = GroupModelRuleSetting{}
	}
	reset()
	t.Cleanup(reset)

	assert.False(t, claudeCacheOptimizationGloballyConfigured())

	stableBaseBreakpointSetting.Rules = []GroupModelRule{{Group: "default", Models: []string{"*"}}}
	assert.True(t, claudeCacheOptimizationGloballyConfigured())
}

// TestCheckClaudeCacheAffinityCoverage exercises the combined self-check
// end-to-end: it must not panic in either state, since it is wired into both
// startup and every relevant option update.
func TestCheckClaudeCacheAffinityCoverage(t *testing.T) {
	previousAffinity := channelAffinitySetting
	previousStableBase := stableBaseBreakpointSetting
	t.Cleanup(func() {
		channelAffinitySetting = previousAffinity
		stableBaseBreakpointSetting = previousStableBase
	})

	stableBaseBreakpointSetting = GroupModelRuleSetting{Rules: []GroupModelRule{{Group: "default", Models: []string{"*"}}}}
	channelAffinitySetting = ChannelAffinitySetting{Enabled: false}
	assert.NotPanics(t, CheckClaudeCacheAffinityCoverage)

	channelAffinitySetting = ChannelAffinitySetting{
		Enabled: true,
		Rules:   []ChannelAffinityRule{{ModelRegex: []string{"^claude-.*$"}}},
	}
	assert.NotPanics(t, CheckClaudeCacheAffinityCoverage)
}
