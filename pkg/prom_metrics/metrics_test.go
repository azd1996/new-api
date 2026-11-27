package prommetrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordRelayUpstreamDelta(t *testing.T) {
	Init()

	// Missing gateway or upstream timings must not create any series.
	RecordRelayUpstreamDelta("claude-3-7-sonnet", "default", "7", 0, 0, 900, 2600)
	RecordRelayUpstreamDelta("claude-3-7-sonnet", "default", "7", 1.2, 3.4, 0, 0)

	families, err := registry.Gather()
	require.Nil(t, err)
	for _, mf := range families {
		assert.NotContains(t, []string{
			"newapi_relay_ttft_upstream_delta_seconds",
			"newapi_relay_duration_upstream_delta_seconds",
		}, mf.GetName(), "a missing side of a comparison must not create a series")
	}

	RecordRelayUpstreamDelta("claude-3-7-sonnet", "default", "7", 1.2, 3.4, 900, 2600)

	collect := func(name string) *dto.Histogram {
		families, err := registry.Gather()
		require.Nil(t, err)
		for _, mf := range families {
			if mf.GetName() != name {
				continue
			}
			require.Len(t, mf.Metric, 1)
			labels := map[string]string{}
			for _, lp := range mf.Metric[0].GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			assert.Equal(t, "claude-3-7-sonnet", labels["model"])
			assert.Equal(t, "default", labels["group"])
			assert.Equal(t, "7", labels["channel"])
			return mf.Metric[0].GetHistogram()
		}
		return nil
	}

	ttft := collect("newapi_relay_ttft_upstream_delta_seconds")
	require.NotNil(t, ttft)
	assert.Equal(t, uint64(1), ttft.GetSampleCount())
	assert.InDelta(t, 1.2-0.9, ttft.GetSampleSum(), 1e-9)

	duration := collect("newapi_relay_duration_upstream_delta_seconds")
	require.NotNil(t, duration)
	assert.Equal(t, uint64(1), duration.GetSampleCount())
	assert.InDelta(t, 3.4-2.6, duration.GetSampleSum(), 1e-9)
}

func TestRecordDrift(t *testing.T) {
	Init()

	// Two settled requests on the same model/group/channel, one of them a mismatch.
	RecordDrift("claude-3-7-sonnet", "default", "7", false)
	RecordDrift("claude-3-7-sonnet", "default", "7", true)

	counter := func(name string) float64 {
		families, err := registry.Gather()
		require.Nil(t, err)
		for _, mf := range families {
			if mf.GetName() != name {
				continue
			}
			require.Len(t, mf.Metric, 1)
			labels := map[string]string{}
			for _, lp := range mf.Metric[0].GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			assert.Equal(t, "claude-3-7-sonnet", labels["model"])
			assert.Equal(t, "default", labels["group"])
			assert.Equal(t, "7", labels["channel"])
			return mf.Metric[0].GetCounter().GetValue()
		}
		return 0
	}

	assert.Equal(t, float64(2), counter("newapi_drift_requests_total"))
	assert.Equal(t, float64(1), counter("newapi_drift_mismatch_total"))
}

func TestRecordUsageCheck(t *testing.T) {
	Init()

	// Four compared streams on one model/group/channel: clean, tampered,
	// unreported, and one that is both.
	RecordUsageCheck("claude-opus-4-8", "default", "9", false, false)
	RecordUsageCheck("claude-opus-4-8", "default", "9", true, false)
	RecordUsageCheck("claude-opus-4-8", "default", "9", false, true)
	RecordUsageCheck("claude-opus-4-8", "default", "9", true, true)

	counter := func(name string) float64 {
		families, err := registry.Gather()
		require.Nil(t, err)
		for _, mf := range families {
			if mf.GetName() != name {
				continue
			}
			require.Len(t, mf.Metric, 1)
			labels := map[string]string{}
			for _, lp := range mf.Metric[0].GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			assert.Equal(t, "claude-opus-4-8", labels["model"])
			assert.Equal(t, "default", labels["group"])
			assert.Equal(t, "9", labels["channel"])
			return mf.Metric[0].GetCounter().GetValue()
		}
		return 0
	}

	assert.Equal(t, float64(4), counter("newapi_usage_check_total"))
	assert.Equal(t, float64(2), counter("newapi_usage_tampered_total"))
	assert.Equal(t, float64(2), counter("newapi_usage_unreported_total"))
}
