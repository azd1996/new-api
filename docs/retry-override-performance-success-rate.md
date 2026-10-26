# retry-override 与性能指标成功率

## 结论与适用范围

本文记录对 new-api 概览、数据看板中“性能指标”成功率的代码核对结果，重点说明渠道自定义 `retry-override` 的影响。

核对日期：2026-10-25。本文描述核对时的实现，后续修改重试循环或结算路径时应重新确认。

在已核对的 Relay 重试循环及文本、音频成功结算路径中，实际命中的 `retry-override` 共用同一次入站请求的重试循环、`relayInfo` 和 request_id。性能指标在最终成功结算或整个循环最终失败时记录结果，中间尝试失败只写相应日志，不在循环内逐次累计失败性能样本。

例如，一个请求经历“失败 → 命中重试覆盖规则 → 重试成功”，性能指标计为一次成功，而不是一次失败加一次成功。

这项结论来自代码路径核对，不代表已经完成所有上游协议的运行时验证，也不代表异步任务提交后的执行结果统计。

## 页面统计来源

概览和数据看板的性能区域都调用 `/api/perf-metrics/summary`，查询窗口固定为最近 24 小时。

后端从性能聚合表 `perf_metrics` 和进程内尚未持久化的性能桶汇总数据，每个模型的成功率为：

```text
模型成功率 = success_count / request_count × 100%
```

性能样本按 `OriginModelName` 归属到调用方原始模型。更换渠道重试不会把同一个请求拆成多个模型请求样本。

该数据源不查询使用日志，也没有保存逐请求 request_id，因此没有在查询时执行 request_id 去重。本文讨论的“最终结果只计一次”来自记录样本的位置。

两个性能区域顶部的汇总成功率目前取各模型成功率的简单平均；它与每个模型按请求计数计算的成功率不同，也不是所有模型请求数加权后的全局成功率。

## 命中 retry-override 后的记录顺序

一次上游尝试返回错误后，`Relay` 依次执行：

1. 调用 `maybeApplyRetryRuleFallback`，匹配渠道的有效重试覆盖规则。
2. 命中时将改写操作暂存到 `PendingRetryRewrite`，并写一条类型为 `8` 的重试覆盖审计日志。
3. 将本次匹配详情直接传给 `processChannelError`，为本次失败写类型为 `5` 的错误日志。
4. 命中规则则 `continue`，回到同一个重试循环尝试下一次调用。
5. 最终成功时通过成功结算记录成功性能样本；循环结束仍失败时记录一次失败性能样本。

步骤 2、3 都不会累计失败性能样本。类型为 `8` 的审计日志也不参与成功率计算。

### 两种规则动作

| 动作 | 行为 | 性能样本 |
|---|---|---|
| `retry_same_channel` | 固定原渠道，设置 `RetryFallbackDone`，通过 `ResetRetryNextTry` 授予一次不消耗普通重试预算的尝试 | 由整个请求最终结果决定 |
| `fallback_next_channel` | 不固定渠道、不重置预算，继续渠道选择；受普通重试预算和渠道可用性限制 | 由整个请求最终结果决定 |

`retry_same_channel` 的守卫是请求级的：一旦设置 `RetryFallbackDone`，后续失败不再进入重试覆盖规则匹配，包括切换渠道动作；后续是否继续普通重试，仍由普通重试逻辑决定。

`fallback_next_channel` 不设置该一次性守卫，可以在后续渠道失败时继续命中重试覆盖规则。命中规则、写出审计日志表示作出了重试决策，不保证预算或渠道一定允许下一次上游调用。

## 常见场景

下表假设相关日志开关开启、错误允许记录，且日志写入成功。消耗日志为类型 `2`，错误日志为类型 `5`，重试覆盖审计日志为类型 `8`。

| 请求经过 | 使用日志 | 性能指标 |
|---|---|---|
| 首次失败命中规则，随后成功 | 1 条审计、1 条带触发标记的错误、1 条最终消耗 | 请求数 +1，成功数 +1 |
| 首次失败命中同渠道规则，再次失败且请求结束 | 1 条审计、首次错误标记为 true、后续未命中规则的错误标记为 false | 请求数 +1，成功数不增加 |
| 多次切换渠道均命中规则，最终成功 | 每次命中各写审计和错误，最后写消耗 | 请求数 +1，成功数 +1 |
| 多次切换渠道均命中规则，最终预算耗尽 | 每次命中各写审计和错误，没有最终成功消耗 | 请求数 +1，成功数不增加 |

最后一种场景中，即使最后一条错误的 `retry_triggered` 仍为 true，整个请求也可能已经最终失败，不能据此判断还会有下一次调用。

### HTTP 200 响应体命中的情况

对于已经由 OpenAI 响应体检查实际识别、转成错误并进入上述循环的 HTTP 200 响应，规则重试发生在正常成功结算之前。不会因为上游 HTTP 状态为 200，就先记一个成功性能样本再重试。

对应错误日志可以出现 `status_code=200`。这里应根据处理结果理解其失败含义，不能把所有 HTTP 200 错误日志都当作成功。

这只说明实际触发后的计数行为，不表示所有协议、流式响应或模板配置都必然能触发响应体检查。规则检测覆盖范围需另行检查。

## 错误日志触发标记的含义

新错误日志将 `retry_triggered` 写入本机真列并保留 `other.retry_triggered`，命中规则时另外写入：

```text
other.admin_info.retry_override
  action
  rule_ids
  rule_descriptions
  status_code
  relay_format
  rewrites_count
  rewrites
  error_message
```

部分详情字段按实际内容出现。规则编号是有效规则列表的下标，模板规则排在渠道内联规则之前。

`retry_triggered=true` 仅表示这一次错误命中了重试覆盖规则，不表示整个请求最终失败，也不表示已经成功执行下一次调用。普通重试不设置该标记。

规则详情按本次尝试直接传递，不依赖一个保留在请求上下文里的触发标记，因此不会把首次命中的规则误带到后续未命中的错误日志中。管理员“触发重试：是 / 否”筛选用于查这类错误；历史未记录该标记的错误归入“否”。

## 与 request_id 最新日志口径的区别

“在查询时间范围内，按 request_id 取最新一条消耗或错误日志，以其类型判定成功失败”是一套日志分析口径。它与当前性能指标的最终结果采样通常一致，但不能视为同一个实现。

| 情况 | 当前性能指标 | 范围内最新日志口径 |
|---|---|---|
| 内部重试后成功，完整过程都在范围内 | 计一次成功 | 最终消耗日志决定成功 |
| 中间错误在范围内，最终成功在范围结束之后 | 不会因那条中间错误记录失败样本；成功样本进入后续采样时间桶 | 范围内可能只有错误，因而判失败 |
| 多次独立入站请求复用同一 request_id | 分别记录性能样本 | 可能合并为一条 |
| 关闭错误或消耗日志，或日志写入失败 | 性能采样不依赖这些日志是否写入 | 统计可能缺失或偏差 |

当前性能查询按时间桶边界过滤，不能当作精确到逐条日志时间的滑动窗口。性能采集开关、刷新和多实例汇总时效也会影响查询可见数据；“重试只计最终结果”不保证与日志报表在任意时刻逐项相等。

如果实现 request_id 最新日志统计，需要单独定义同秒多条记录的排序、时间范围边界、空 request_id、重复 ID 和日志类型范围。类型 `8` 的审计不能参与最终成功或失败判定；也不能仅按秒级时间戳取最新，忽略并列记录。

相关方案见 [使用日志统计模式方案](usage-log-statistics-design.md)。该方案与本文记录的现有性能指标实现应分别理解。

## 后续核对入口

代码行号会变化，优先按下列函数名查找：

| 文件 | 入口或函数 | 核对内容 |
|---|---|---|
| `controller/relay.go` | `Relay`、`processChannelError` | 规则匹配、错误日志、continue、循环外最终失败采样 |
| `controller/retry_fallback.go` | `maybeApplyRetryRuleFallback`、`recordRetryOverrideAudit` | 两种动作、守卫、审计详情 |
| `service/channel_select.go` | `RetryParam` 相关方法 | 重试预算及一次性预算重置 |
| `relay/common/override.go` | `ApplyParamOverrideWithRelayInfo` | 待应用改写的消费与清除 |
| `relay/channel/openai/retry_body_guard.go` | 响应体检查 | HTTP 200 响应体触发入口 |
| `service/text_quota.go` | `PostTextConsumeQuota` | 文本成功结算及成功采样 |
| `service/quota.go` | `RecordRelaySample` 调用处 | 音频成功结算及成功采样 |
| `pkg/perf_metrics/metrics.go` | `RecordRelaySample`、`QuerySummaryAll` | 原始模型归属、采样、分模型成功率 |
| `model/perf_metric.go` | `PerfMetric`、汇总查询 | 性能聚合表及时间桶 |
| `web/default/src/features/dashboard/components/models/performance-overview.tsx` | `PerformanceOverview` | 数据看板性能区域、24 小时查询、顶部平均值 |
| `web/default/src/features/dashboard/components/overview/performance-health-panel.tsx` | `PerformanceHealthPanel` | 概览性能区域、24 小时查询、顶部平均值 |

现有 `controller/retry_fallback_test.go` 覆盖规则动作、错误记录和匹配信息不串到后续错误的行为；完整重试循环下性能样本数量的集成测试尚未补充。后续若调整采样位置，应重点验证“命中后成功只记一次成功”“最终失败只记一次失败”“200 响应体命中不提前记成功”。

规则整体设计见 [retry-override 设计](retry-override-design.md)。
