# 重试覆盖规则（Retry Override）方案文档

> 本文由 `.comate/specs/` 下 5 篇设计文档整合而成，描述「重试覆盖」能力的完整方案：上游报错（或 200 响应体内含错误）时，按规则匹配 → 改写请求体 → 在原渠道或下一渠道重试。
>
> 来源 spec（按演进顺序）：
> 1. `thinking-signature-fallback` — 起点：错误触发式兜底重试引擎
> 2. `retry-override-fallback-action` — 新增「兜底到下一渠道」动作
> 3. `retry-override-body-detection` — GPT 接线 + 200 响应体错误检测（非流式/流式）
> 4. `retry-override-phased-rules` — 重构为显式四段式规则
> 5. `retry-override-stream-precommit-buffer` — 流式检测改为「提交前缓冲」

---

## 2. 核心模型：四段式规则

一条规则（`dto.RetryRule`）由四段组成，按顺序**短路**：任一判定阶段不通过则整条跳过。规则数组按**优先级排序**，一次响应只采用**首条**phase1&2 均命中的规则（命中即停，由该规则的 phase3+phase4 全权决定结果）。

- **phase1 请求侧过滤**：匹配「请求体 + 请求上下文」（`model`/`upstream_model`/`original_model`/`user_group`/`token_group`/`using_group`/`request_path`/`retry_index`/`is_retry`/`last_error.*` + `relay_format`）。不满足 → 跳过本规则。
- **phase2 响应侧判定**：匹配响应上下文 `status_code`/`error_message`/`response_body`/`relay_format`/`model`。不满足 → 不重试、不改写。
- **phase3 条件化改写**：phase1&2 通过后，按数组顺序把每个改写算子应用到请求体（复用 param-override 算子，如 `prune_objects`/`delete`/`set`）。空数组 = 纯「检测 + 动作」。
- **phase4 重试动作**：`retry_same_channel`（默认）、`fallback_next_channel`，或 `return_error`（终止重试，向调用方返回指定错误码/消息）。

关键设计：**phase1/2/3 条件语义与「参数覆盖」完全一致**（复用同一求值器，无第二套条件语法），改写也直接复用 param-override 算子。重试覆盖相对参数覆盖真正的净增只有：挂在「上游报错之后」的时机 + 改写后回原渠道/下一渠道重试。

## 3. 配置 Schema

渠道级 `setting.retry_override`，恒为规则对象数组 `[]RetryRule`：

```json
[
  {
    "description": "可选，仅说明，不参与匹配",
    "phase1_request_condition":   { "conditions": [ ... ], "logic": "AND|OR" },
    "phase2_response_conditions": { "conditions": [ ... ], "logic": "AND|OR" },
    "phase3_request_rewrite":     [ { "mode": "...", "path": "...", "value": ... } ],
    "phase4_retry_action": "retry_same_channel | fallback_next_channel | return_error"
  }
]
```

`phase4_retry_action` 兼容两种写法：**字符串**（如 `"fallback_next_channel"`）或**对象** `{"action": "...", "status_code": 429, "status_msg": "..."}`。`status_code`/`status_msg` 仅对 `return_error` 生效——终止重试并把该状态码（及可选消息）返回给调用方；`status_code` 缺省/越界时回退 **429**。对象/字符串可混用，序列化时仅动作（无 status）回落为字符串形式，保持旧配置不变。

约束：`phase3_request_rewrite` 恒为数组（可空=只检测不改写）；phase1/phase2 可省略（省略即恒通过）；`logic` 省略默认 **OR**；`phase4_retry_action` 省略默认 `retry_same_channel`，非法/未知动作回退 `retry_same_channel`（解析容错，绝不使整段 setting 失效）。

## 4. 条件语法（与参数覆盖一致）

phase1/2/3 条件统一走 `relay/common` 求值器（`EvaluateConditions`，包装 `checkConditions`）：

- mode：`full`/`prefix`/`suffix`/`contains`/`gt`/`gte`/`lt`/`lte`（数值按 gjson 类型比较）。
- 每条支持 `invert`、`pass_missing_key`。
- `logic` 默认 **OR**，可显式 `AND`。
- 路径解析：先查「当前文档」（phase1=请求体、phase2=响应上下文、phase3=请求体节点），查不到回落上下文。

---

## 5. 重试动作与一次性语义

两种动作在主循环（`controller/relay.go`）里的差异：

| | retry_same_channel（默认） | fallback_next_channel |
|---|---|---|
| 钉当前渠道（`SetPinnedChannel`，soft-pin） | 是 | 否 |
| `ResetRetryNextTry`（不消耗预算） | 是 | 否（`IncreaseRetry` 推进档位选下一渠道） |
| 应用 phase3 改写 | 是（发回原渠道） | 是（发给下一渠道） |
| 绕过「400 不可重试」门 | 是 | 是 |
| 依赖 `RetryTimes` 预算 | 否（一次性） | 是（靠 `GetRetry() <= RetryTimes` 推进档位） |

- **soft-pin 原语**（`service/channel_pin.go`）区别于会直接关闭重试的 `specific_channel_id`。选原渠道而非从头重排的理由：兜底常因高优渠道满负荷才切下来，重排大概率又打回满负荷渠道；问题只在请求形态，原渠道改写后即可通过。
- **一次性语义拆分**：`retry_same_channel` 一次性（`RetryFallbackDone` 保证整请求最多触发一次，防同渠道死循环）；`fallback_next_channel` **不受一次性限制**，可连续多跳直到最后一个渠道，由 `RetryTimes` 预算与渠道用尽收敛（要跳完所有渠道，`RetryTimes` 须 ≥ 可用渠道数 − 1）。
- **第三种动作 `return_error`**：命中即终止——不钉渠道、不重置预算、不改写请求体；把失败响应的 `apiErr` 重写为配置的 `status_code`（缺省 429）与可选 `status_msg`，主循环直接 `break`，由顶层 deferred 把该错误返回给调用方。审计日志额外记 `return_status`/`return_message`，`status_code` 仍记原始上游状态码（触发原因）。
- **动作裁决（首条命中生效）**：规则按数组顺序优先级排列，取**首条**phase1&2 均命中的规则，由它的 phase4 动作与 phase3 改写全权决定，命中即停、不再看后续规则；无规则命中则不触发（默认 `retry_same_channel` 语义）。（历史行为是「任一命中为 fallback 则整体 fallback、phase3 跨规则叠加」，现已改为首条命中。）

## 6. 200 响应体错误检测

200 被判成功、不走错误路径，需在「写客户端之前」主动检测 body：

- **条件**：用 phase2 的 `response_body` 条件匹配 200 body（`SuccessContext` 产出 `{relay_format, status_code, response_body}`）。
- **非流式**：成功 handler 在 `IOCopyBytesGracefully` 之前跑 `ShouldTriggerOnBody`；命中则合成一个可重试 `*types.NewAPIError`，handler 返回 error，进入重试覆盖钩子。
- **流式（提交前缓冲 / 方案 A）**：确认头部若干 chunk 无错误前**不向客户端发任何字节**。
  - 未提交态：每个 chunk 先扫描；命中 → 置 matched、`sr.Stop`、客户端零输出、干净换渠道（等价从头重来）；未命中 → 存 buffer。
  - 遇首个「内容事件」（Responses 的 `output_item.added`/`output_text.delta` 等；Chat 的带 content/tool_calls/reasoning 的 delta）或达到块数上限 → 提交：按序 flush buffer，之后转即发即透传。
  - 提交后不再拦截：内容已开始流出后，中途上游错误如实透传（不再静默兜底）。
  - 块数上限：常量 `retryOverridePrecommitMaxChunks = 3`；渠道可配 `retry_override_stream_precommit_chunks`（未填/0 按 3）。
- **动机**：旧「即发即检」会导致前导块（如 `response.created`）已 flush，兜底渠道 B 的前导与客户端已收到的 A 前导跨渠道拼接、`response_id`/`item_id` 体系对不上，严格解析的客户端（Codex 类）协议错乱。方案 A 彻底消除续写拼接，并因此删除了 `RetryContinuationContentSent`、`dropPreamblePending`、`RetryOverrideDropDuplicatePreamble`。

---

## 7. 存储

规则是**每渠道**配置，存进 `dto.ChannelSettings`（渠道表 `setting` JSON 列），**零 schema 迁移、无需新列**。理由：每渠道、无「按 tag 批量下发」诉求、可结构化避免 JSON-in-JSON、对 fork 合并最友好（这也是 `ThinkingToContent` 等每渠道开关的惯例）。取值链路：重试循环里失败渠道对象就在手边，`channel.GetSetting().RetryOverride` 直接拿到，与「原渠道重试」天然一致。

## 8. 关键落点与数据流

**求值器归属**：导出 `relay/common.EvaluateConditions` 给 `retryrule` 用（不反向内聚）。确认无 import 循环（`relay/common` 不依赖 `retryrule`）。

**改写接线（公共入口）**：`relay/common/override.go` 的 `ApplyParamOverrideWithRelayInfo` 在应用完 `info.ParamOverride` 后**追加应用 `info.PendingRetryRewrite`**（应用后置 nil），使所有调用该入口的格式（claude/responses/chat_via_responses/compatible/gemini/…）统一获得改写，不再只在 claude handler 生效。

**错误路径数据流**：
1. 上游返回错误 → 主循环命中 `controller/retry_fallback.go` 的 `maybeApplyRetryRuleFallback` → 构造 reqCtx（`BuildParamOverrideContext` + relay_format）与 respCtx（注入 model）。
2. `retryrule.CollectRewrites(rules, reqCtx, respCtx)` 返回 `Decision{Matched, Rewrites, Action, ReturnStatus, ReturnMessage, MatchedIndex}`：phase1 用 reqCtx、phase2 用 respCtx；取首条命中规则，用其 phase3 改写与 phase4 动作。
3. 命中后按动作分流：`same` → 暂存 `PendingRetryRewrite`、标记 `RetryFallbackDone`、pin + reset，`return (retry=true, terminate=false)`，主循环 `continue`；`fallback` → 暂存改写、直接放行，`continue`；`return_error` → 重写 `apiErr` 状态码/消息，`return (retry=false, terminate=true)`，主循环 `break`。
4. 下一次迭代由 relay handler 在公共入口应用改写后发出（terminate 不进入下一次迭代）。

**成功路径（200 检测）**：`relay/channel/openai/retry_body_guard.go` 的 `precommitGuard`——无 `retry_override` 规则时初始即 committed（零开销透传）；有规则则按 §6 缓冲 + 扫描 + 提交。

## 9. 边界与异常

- 无 `retry_override` / 解析失败：视为空，**禁用 + warn**，不影响正常转发与用户模型调用（无迁移，运维手工重填）。
- phase 省略 → 恒通过；`logic` 省略 → OR；`phase4` 非法/未知 → `retry_same_channel`（容错解析，不使整段 setting 失效）；`return_error` 缺省/越界 `status_code` → 429。
- `return_error` 走正常错误返回路径：预扣费在顶层 deferred 里照常退还，不产生计费。
- `fallback_next_channel` 但 `RetryTimes==0` 或无其他可用渠道 → 请求以原始错误结束（如实反映，不额外造预算）。
- 命中错误不外发：逐渠道兜底，**仅当重试到最后一个渠道仍失败**才暴露最后渠道的错误；流式下客户端若已收到正文，则在末尾追加最后错误。
- body 检测限制单块体量（`retryBodyGuardMaxBytes` 16KB），避免性能问题。

## 10. 与早期 spec 的差异（重要）

spec `retry-override-body-detection` / `phased-rules` 描述过「**200-body 触发强制 `fallback_next_channel`**」（在 `CollectRewrites` 内写死，忽略配置的 phase4）。该特化**已被移除**：现在 200 命中也遵循规则自身的 `phase4_retry_action`（见 `relay/retryrule/retryrule.go` 的 `CollectRewrites` 注释与 commit `dc5b147 feat(retry): modify retry rule when status200`）。本文第 5/6 节描述的是**当前**行为；阅读旧 spec 时请以此为准。
