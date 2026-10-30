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

	httpRequests     *prometheus.CounterVec
	httpDuration     *prometheus.HistogramVec
	relayRequests    *prometheus.CounterVec
	promptTokens     *prometheus.CounterVec
	completionTokens *prometheus.CounterVec
	relayDuration    *prometheus.HistogramVec
	relayTtft        *prometheus.HistogramVec
	quotaUsed        *prometheus.CounterVec
	quotaPerUnit     prometheus.Gauge
	errorsTotal      *prometheus.CounterVec
	// Reporting-only settlement amounts (quota units; never affect billing).
	// channel/customer are monotonic non-negative counters; profit can decrease
	// (customer discount below channel cost) so it is a gauge.
	channelStatQuota  *prometheus.CounterVec
	customerStatQuota *prometheus.CounterVec
	profitQuota       *prometheus.GaugeVec

	// errorHighCardLabels gates the high-cardinality error labels (model and
	// customer). When false their values are blanked so a large model/customer
	// space cannot explode the error metric's series count.
	errorHighCardLabels = true
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
		relayTtft = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "newapi_relay_ttft_seconds",
			Help:    "Upstream relay time-to-first-token in seconds (streaming responses only).",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60},
		}, []string{"model", "group", "channel"})
		quotaUsed = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_relay_quota_used_total",
			Help: "Total quota (internal unit) consumed by relay requests.",
		}, []string{"model", "group", "channel"})
		quotaPerUnit = prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "newapi_quota_per_unit",
			Help: "Quota units per 1 USD; divide quota by this value to get the amount in USD.",
		})
		errorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_error_total",
			Help: "Total upstream/relay errors by detailed-error short name, attribution and request dimensions.",
		}, []string{"short_name", "attribution", "model", "channel", "status", "protocol", "stream", "customer"})
		channelStatQuota = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_channel_stat_quota_total",
			Help: "Reporting-only channel-discounted amount in quota units (divide by newapi_quota_per_unit for USD). Never affects billing.",
		}, []string{"model", "group", "channel"})
		customerStatQuota = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "newapi_customer_stat_quota_total",
			Help: "Reporting-only customer-discounted amount in quota units, for requests with a decoded customer. Never affects billing.",
		}, []string{"model", "group", "channel"})
		profitQuota = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "newapi_profit_quota",
			Help: "Reporting-only cumulative profit in quota units (customer-discounted minus channel-discounted) over customer-bearing requests; may decrease and resets on restart.",
		}, []string{"model", "group", "channel"})
		registry.MustRegister(httpRequests, httpDuration, relayRequests,
			promptTokens, completionTokens, relayDuration, relayTtft, quotaUsed, quotaPerUnit, errorsTotal,
			channelStatQuota, customerStatQuota, profitQuota)
	})
}

// SetErrorHighCardLabels toggles whether the error metric carries real model
// and customer label values. Disable it when those spaces are large enough to
// risk a Prometheus cardinality blow-up; the label names stay present (with
// empty values) so the series schema is stable either way.
func SetErrorHighCardLabels(enabled bool) {
	errorHighCardLabels = enabled
}

// RecordError records one error occurrence. shortName / attribution are the
// admin-configured category / attribution ("" becomes "uncategorized"); the
// remaining labels describe the request. No-op when metrics are disabled.
func RecordError(shortName, attribution, model, channel, status, protocol, stream, customer string) {
	if registry == nil {
		return
	}
	if shortName == "" {
		shortName = "uncategorized"
	}
	if attribution == "" {
		attribution = "uncategorized"
	}
	if !errorHighCardLabels {
		model = ""
		customer = ""
	}
	errorsTotal.WithLabelValues(shortName, attribution, model, channel, status, protocol, stream, customer).Inc()
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
// quota are all finalized at the billing settlement point. ttftSeconds is the
// streaming time-to-first-token (0 for non-streaming or when no response was
// observed) and is only recorded when positive. When metrics are disabled this
// returns immediately with zero overhead.
func RecordRelay(model, group, channel, result string,
	promptTk, completionTk int, durationSeconds float64, quota int, ttftSeconds float64) {
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
	if ttftSeconds > 0 {
		relayTtft.WithLabelValues(model, group, channel).Observe(ttftSeconds)
	}
	if quota > 0 {
		quotaUsed.WithLabelValues(model, group, channel).Add(float64(quota))
	}
}

// RecordStatAmounts records the reporting-only settlement amounts (quota units)
// for one relay: the channel-discounted amount, and—when the request carried a
// decoded customer—the customer-discounted amount and the profit (customer
// minus channel, which may be negative). These never affect billing. No-op when
// metrics are disabled.
func RecordStatAmounts(model, group, channel string, hasCustomer bool, channelStat, customerStat int) {
	if registry == nil {
		return
	}
	if channelStat > 0 {
		channelStatQuota.WithLabelValues(model, group, channel).Add(float64(channelStat))
	}
	if hasCustomer {
		if customerStat > 0 {
			customerStatQuota.WithLabelValues(model, group, channel).Add(float64(customerStat))
		}
		profitQuota.WithLabelValues(model, group, channel).Add(float64(customerStat - channelStat))
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
