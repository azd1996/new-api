# new-api Prometheus 仪表盘 PromQL 推荐

本文基于 `pkg/prom_metrics/metrics.go` 中实际注册的指标与标签编写，给出一套可直接用于
Grafana 仪表盘的 PromQL。所有示例均假设采集器额外注入了 `instance`、`job` 两个标签，
示例 job 为 `newapi-31525`。

## 0. 前置约定

### 采集器附加标签
你的 exporter 配置额外加了：

```
labels = { instance="$hostname", job="newapi-31525" }
```

因此每条时间序列都带 `instance` 与 `job`。本文统一用一个基础选择器 `$base`
表示范围过滤，建议在 Grafana 里用模板变量实现：

- `job` 固定为 `newapi-31525`（或做成下拉变量）
- `instance` 做成多选变量 `$instance`（`label_values(newapi_http_requests_total, instance)`）

下文 PromQL 里出现的 `job="newapi-31525", instance=~"$instance"` 即为此基础选择器，
单机时可简化为 `job="newapi-31525"`。

### 指标与标签总览

| 指标 | 类型 | 标签（业务标签，另有 instance/job） |
| --- | --- | --- |
| `newapi_http_requests_total` | Counter | `method`, `path`, `status` |
| `newapi_http_request_duration_seconds_*` | Histogram | `method`, `path` |
| `newapi_relay_requests_total` | Counter | `model`, `group`, `channel`, `result`(`success`/`failure`) |
| `newapi_relay_prompt_tokens_total` | Counter | `model`, `group`, `channel` |
| `newapi_relay_completion_tokens_total` | Counter | `model`, `group`, `channel` |
| `newapi_relay_duration_seconds_*` | Histogram | `model`, `group`, `channel` |
| `newapi_relay_quota_used_total` | Counter | `model`, `group`, `channel` |
| `newapi_quota_per_unit` | Gauge | 无（配额/美元换算基数） |
| `newapi_error_total` | Counter | `short_name`, `model`, `channel`, `status`, `protocol`, `stream`, `customer` |

说明：
- `channel` 是渠道的数字 ID（字符串形式），非渠道名。
- `group` 是请求所用分组（`UsingGroup`）。
- `newapi_relay_duration_seconds` 的桶为 `0.5,1,2,5,10,20,30,60,120`（上游时延，偏长尾）；
  `newapi_http_request_duration_seconds` 用默认桶（面向网关整体 HTTP 时延）。
- `newapi_error_total` 的 `model`/`customer` 在高基数保护开启时可能为空串（见 `SetErrorHighCardLabels`）。
- `newapi_relay_quota_used_total` 可能未出现在你的截图里：只有产生过计费配额消耗后才会有该序列。

> 时间窗口统一用变量 `$__rate_interval`（Grafana 自动匹配 scrape interval），
> 固定值场景可替换为 `5m`。

---

## 1. 概览行（Overview / Stat 面板）

### 1.1 总请求速率（QPS，网关 HTTP 维度）
```promql
sum(rate(newapi_http_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 1.2 中继请求速率（relay QPS）
```promql
sum(rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 1.3 中继成功率（%）
```promql
100 * sum(rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance", result="success"}[$__rate_interval]))
/
sum(rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 1.4 中继失败率（%）
```promql
100 * sum(rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance", result="failure"}[$__rate_interval]))
/
sum(rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 1.5 HTTP 5xx 比例（网关自身错误）
```promql
100 * sum(rate(newapi_http_requests_total{job="newapi-31525", instance=~"$instance", status=~"5.."}[$__rate_interval]))
/
sum(rate(newapi_http_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 1.6 当前错误速率（errors/s）
```promql
sum(rate(newapi_error_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

---

## 2. 流量与请求（Traffic）

### 2.1 按 HTTP 状态码分解的请求速率（堆叠时序）
```promql
sum by (status) (rate(newapi_http_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 2.2 按路由（path）分解的请求速率（Top N）
```promql
topk(10, sum by (path) (rate(newapi_http_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 2.3 中继请求速率按模型分解（Top N）
```promql
topk(10, sum by (model) (rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 2.4 中继请求速率按分组 / 渠道分解
```promql
sum by (group) (rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```
```promql
topk(10, sum by (channel) (rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 2.5 成功 vs 失败（按 result 堆叠）
```promql
sum by (result) (rate(newapi_relay_requests_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

---

## 3. 时延（Latency）

Histogram 用 `histogram_quantile` + `rate(..._bucket)`。注意 `_bucket` 指标才带 `le` 标签。

### 3.1 网关 HTTP 时延 P50/P90/P99（总体）
```promql
histogram_quantile(0.50, sum by (le) (rate(newapi_http_request_duration_seconds_bucket{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```
```promql
histogram_quantile(0.90, sum by (le) (rate(newapi_http_request_duration_seconds_bucket{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```
```promql
histogram_quantile(0.99, sum by (le) (rate(newapi_http_request_duration_seconds_bucket{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 3.2 上游中继时延 P50/P90/P99（总体）
```promql
histogram_quantile(0.50, sum by (le) (rate(newapi_relay_duration_seconds_bucket{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```
```promql
histogram_quantile(0.90, sum by (le) (rate(newapi_relay_duration_seconds_bucket{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```
```promql
histogram_quantile(0.99, sum by (le) (rate(newapi_relay_duration_seconds_bucket{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 3.3 上游中继 P99 时延按模型分解（定位慢模型）
```promql
histogram_quantile(0.99, sum by (le, model) (rate(newapi_relay_duration_seconds_bucket{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 3.4 平均时延（sum / count，便于粗看）
```promql
sum(rate(newapi_relay_duration_seconds_sum{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
/
sum(rate(newapi_relay_duration_seconds_count{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 3.5 慢请求占比（上游中继 > 10s 的比例，%）
```promql
100 * (
  sum(rate(newapi_relay_duration_seconds_count{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
  -
  sum(rate(newapi_relay_duration_seconds_bucket{job="newapi-31525", instance=~"$instance", le="10"}[$__rate_interval]))
)
/
sum(rate(newapi_relay_duration_seconds_count{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

---

## 4. Token 用量（Tokens）

### 4.1 Prompt / Completion token 速率（tokens/s）
```promql
sum(rate(newapi_relay_prompt_tokens_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```
```promql
sum(rate(newapi_relay_completion_tokens_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 4.2 总 token 吞吐（prompt + completion）
```promql
sum(rate(newapi_relay_prompt_tokens_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
+
sum(rate(newapi_relay_completion_tokens_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 4.3 按模型分解的 completion token 速率（Top N）
```promql
topk(10, sum by (model) (rate(newapi_relay_completion_tokens_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 4.4 区间内累计 token（用于“今日消耗”类 Stat，配合时间范围）
```promql
sum(increase(newapi_relay_prompt_tokens_total{job="newapi-31525", instance=~"$instance"}[$__range]))
+
sum(increase(newapi_relay_completion_tokens_total{job="newapi-31525", instance=~"$instance"}[$__range]))
```

---

## 5. 配额 / 费用（Quota & Cost）

`newapi_relay_quota_used_total` 是内部配额单位的累计；`newapi_quota_per_unit` 是“每 1 美元
对应多少配额”，用配额除以它得到美元金额。

### 5.1 配额消耗速率（内部单位/s）
```promql
sum(rate(newapi_relay_quota_used_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 5.2 费用速率（USD/s）
```promql
sum(rate(newapi_relay_quota_used_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
/
max(newapi_quota_per_unit{job="newapi-31525", instance=~"$instance"})
```

### 5.3 区间内累计费用（USD，配合 Grafana 时间范围 $__range）
```promql
sum(increase(newapi_relay_quota_used_total{job="newapi-31525", instance=~"$instance"}[$__range]))
/
max(newapi_quota_per_unit{job="newapi-31525", instance=~"$instance"})
```

### 5.4 费用 Top 模型（USD/s）
```promql
topk(10,
  sum by (model) (rate(newapi_relay_quota_used_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
) / scalar(max(newapi_quota_per_unit{job="newapi-31525", instance=~"$instance"}))
```

### 5.5 费用 Top 分组 / 渠道（USD/s）
```promql
topk(10,
  sum by (group) (rate(newapi_relay_quota_used_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
) / scalar(max(newapi_quota_per_unit{job="newapi-31525", instance=~"$instance"}))
```

---

## 6. 错误分析（Errors）

`newapi_error_total` 的 `short_name` 是管理员配置的错误简名归类（空归类为 `uncategorized`），
`status` 为上游/响应状态码，`protocol` 为中继协议，`stream` 为 `true`/`false`，
`customer` 为下游客户（可能被高基数保护置空）。

### 6.1 错误速率按简名分类（堆叠，定位主因）
```promql
sum by (short_name) (rate(newapi_error_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 6.2 错误 Top 模型
```promql
topk(10, sum by (model) (rate(newapi_error_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 6.3 错误 Top 渠道
```promql
topk(10, sum by (channel) (rate(newapi_error_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval])))
```

### 6.4 错误按状态码分解
```promql
sum by (status) (rate(newapi_error_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 6.5 错误按协议 / 是否流式分解
```promql
sum by (protocol, stream) (rate(newapi_error_total{job="newapi-31525", instance=~"$instance"}[$__rate_interval]))
```

### 6.6 错误 Top 客户（高基数标签开启时才有值）
```promql
topk(10, sum by (customer) (rate(newapi_error_total{job="newapi-31525", instance=~"$instance", customer!=""}[$__rate_interval])))
```

---

## 7. 存活 / 采集健康

### 7.1 实例在线数
```promql
count(up{job="newapi-31525"} == 1)
```

### 7.2 各实例抓取状态
```promql
up{job="newapi-31525", instance=~"$instance"}
```

### 7.3 quota_per_unit 当前值（Stat，校验换算基数是否正常）
```promql
max(newapi_quota_per_unit{job="newapi-31525", instance=~"$instance"})
```

---

## 8. 建议的 Grafana 模板变量

- `job`：Constant，值 `newapi-31525`。
- `instance`：Query，`label_values(up{job="$job"}, instance)`，多选 + All。
- `model`：Query，`label_values(newapi_relay_requests_total{job="$job"}, model)`，多选 + All（用于下钻）。
- `group`：Query，`label_values(newapi_relay_requests_total{job="$job"}, group)`，多选 + All。

启用 `model`/`group` 变量后，可把上面各查询的选择器扩展为
`{job="$job", instance=~"$instance", model=~"$model", group=~"$group"}` 做联动下钻。

---

## 9. 推荐仪表盘布局

1. **概览行（Stat）**：QPS、relay QPS、成功率、失败率、5xx%、errors/s、当前费用速率、在线实例数。
2. **流量行（Time series）**：按 status / path / model 的请求速率，success vs failure。
3. **时延行（Time series + Heatmap）**：HTTP 与 relay 的 P50/P90/P99；慢请求占比。
4. **Token 行（Time series + Stat）**：prompt/completion 速率、总吞吐、区间累计。
5. **费用行（Time series + Stat + Table）**：费用速率、区间累计、Top 模型/分组/渠道费用表。
6. **错误行（Time series + Table）**：按 short_name 堆叠、Top 模型/渠道/客户、状态码分布。
