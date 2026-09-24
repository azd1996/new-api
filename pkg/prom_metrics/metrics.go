package prommetrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	once     sync.Once
	registry *prometheus.Registry

	httpRequests  *prometheus.CounterVec
	httpDuration  *prometheus.HistogramVec
	relayRequests *prometheus.CounterVec
	promptTokens  *prometheus.CounterVec
	completionTokens *prometheus.CounterVec
	relayDuration *prometheus.HistogramVec
	quotaUsed     *prometheus.CounterVec
	quotaPerUnit  prometheus.Gauge
)

// Init registers all collectors into a dedicated registry. It is safe to call
// multiple times; only the first call takes effect. Init is only invoked when
// metrics export is enabled, so an unset registry means "disabled".
func Init() {
	once.Do(func() {
		registry = prometheus.NewRegistry()
		httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_http_requests_total",
			Help: "Total number of HTTP requests handled.",
		}, []string{"method", "path", "status"})
		httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path"})
		relayRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_relay_requests_total",
			Help: "Total number of relay requests by result.",
		}, []string{"model", "group", "channel", "result"})
		promptTokens = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_relay_prompt_tokens_total",
			Help: "Total prompt tokens consumed by relay requests.",
		}, []string{"model", "group", "channel"})
		completionTokens = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_relay_completion_tokens_total",
			Help: "Total completion tokens consumed by relay requests.",
		}, []string{"model", "group", "channel"})
		relayDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_relay_duration_seconds",
			Help:    "Upstream relay latency in seconds.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120},
		}, []string{"model", "group", "channel"})
		quotaUsed = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_relay_quota_used_total",
			Help: "Total quota (internal unit) consumed by relay requests.",
		}, []string{"model", "group", "channel"})
		quotaPerUnit = prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "newapi_quota_per_unit",
			Help: "Quota units per 1 USD; divide quota by this value to get the amount in USD.",
		})
		registry.MustRegister(httpRequests, httpDuration, relayRequests,
			promptTokens, completionTokens, relayDuration, quotaUsed, quotaPerUnit)
	})
}

// Enabled reports whether metrics collection is active.
func Enabled() bool {
	return registry != nil
}

// Handler returns an HTTP handler that serves the metrics registry.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// SetQuotaPerUnit publishes the quota-per-USD conversion base so dashboards can
// convert consumed quota into a monetary amount.
func SetQuotaPerUnit(v float64) {
	if registry == nil {
		return
	}
	quotaPerUnit.Set(v)
}

// RecordRelay records one relay settlement sample. model/group/channel/tokens/
// quota are all finalized at the billing settlement point. When metrics are
// disabled this returns immediately with zero overhead.
func RecordRelay(model, group, channel, result string,
	promptTk, completionTk int, durationSeconds float64, quota int) {
	if registry == nil {
		return
	}
	relayRequests.WithLabelValues(model, group, channel, result).Inc()
	if promptTk > 0 {
		promptTokens.WithLabelValues(model, group, channel).Add(float64(promptTk))
	}
	if completionTk > 0 {
		completionTokens.WithLabelValues(model, group, channel).Add(float64(completionTk))
	}
	if durationSeconds > 0 {
		relayDuration.WithLabelValues(model, group, channel).Observe(durationSeconds)
	}
	if quota > 0 {
		quotaUsed.WithLabelValues(model, group, channel).Add(float64(quota))
	}
}

// ObserveHTTP records one HTTP request sample. path should be the route template
// (gin FullPath) to keep label cardinality bounded.
func ObserveHTTP(method, path, status string, latencySeconds float64) {
	if registry == nil {
		return
	}
	httpRequests.WithLabelValues(method, path, status).Inc()
	httpDuration.WithLabelValues(method, path).Observe(latencySeconds)
}
