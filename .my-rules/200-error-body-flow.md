# 重试覆盖：HTTP 200 响应体内含错误的处理流程

针对形如"上游返回 HTTP 200，但响应体里其实带着错误消息（如限流）"的规则（对应 `my-retry-rules.json` 第 2 条：`gpt-5.6-sol` + `openai_responses`，200 里含 `exceeded rate limit`，动作 `fallback_next_channel`）。

这条链路与普通 4xx/5xx 错误不同，多了一个"成功路径拦截"的前置环节：正常 200 会被当成功响应直接写回客户端，所以必须在写回前先拦一道。

## 完整流程

### ① 上游返回 HTTP 200，body 内含错误文案
不拦截的话会被当成功响应直接转发给客户端。

### ② 写回前的 body 拦截：`retryBodyGuardError`
Responses 响应处理器在解析/转发前先调用 `retryBodyGuardError(info, 200, body)`：

- 非流式：`relay/channel/openai/relay_responses.go:30`、`relay/channel/openai/chat_via_responses.go:37`
- 流式：`relay/channel/openai/relay_responses.go:95`、`relay/channel/openai/chat_via_responses.go:304`（逐 chunk 扫描）

guard 内部（`relay/channel/openai/retry_body_guard.go`）：
- `len(ops)==0` 直接返回 nil（无规则零开销）
- body 只扫前 **16KB**（`retryBodyGuardMaxBytes`）
- 构造 `respCtx = SuccessContext(relay_format, 200, body片段)`，body 放在 **`response_body`** 键下
- 调 `ShouldTriggerOnBody`，对每条规则跑 phase1(reqCtx) + phase2(respCtx)：
  - phase1：`model` 前缀 `gpt-5.6-sol`、`relay_format=openai_responses`
  - phase2：`status_code=200`、`response_body` 包含 `exceeded rate limit`
  - 命中 → 返回 true

### ③ 合成可重试错误 + 吞掉这段 body
guard 命中后返回 `NewErrorWithStatusCode(errors.New(body片段), ErrorCodeBadResponseStatusCode, 200)`：
- **这段错误 body 绝不会写回客户端**（非流式直接 return err；流式吞掉该 chunk、`sr.Stop`，不转发不入库）。
- 否则客户端会把错误文本当成一次"成功"响应收下。

### ④ 进重试兜底 hook：`maybeApplyRetryRuleFallback`
relay 主循环拿到该"错误"，进入 `controller/retry_fallback.go`：
- `respCtx = ResponseContext(200, errorMessage=body, relayFormat)`——当 `status_code==200` 时会同时把 body 塞进 `error_message` 和 `response_body`（`relay/retryrule/retryrule.go:48-50`），因此 phase2 的 `response_body contains` 在这一步也继续命中。
- `CollectRewrites` 命中该规则；它没有 phase3，不 stage 任何请求改写（正确：请求本身没问题，只是渠道被限流）。

### ⑤ 强制 `fallback_next_channel`
`CollectRewrites` 中有硬规则（`relay/retryrule/retryrule.go:112-114`）：只要命中触发的 `status_code==200`，action 被**强制**改成 `fallback_next_channel`，无视配置里写的值。

随后在 hook 里（`controller/retry_fallback.go:74-82`）走 fallback 分支：不设 `RetryFallbackDone`、不 pin、不重置预算 → 返回 true 绕过"200 不可重试"这道门 → 主循环 `IncreaseRetry` 抬优先级，`getChannel` 选下一个渠道。这是**多跳**的，受 `RetryTimes` 和可用渠道数约束；下一个渠道若又 200 限流，再次触发继续往下兜，直到成功或耗尽。

## 注意事项

- **为什么强制换渠道**：同一个被限流的渠道立刻重试没意义，故对 200 一律 fallback。配置写 `fallback_next_channel` 与此一致（写别的也会被强制覆盖）。
- **16KB 上限**：限流文案一般在 body 顶部，没问题；若标记出现在 16KB 之后则扫不到。
- **流式边界**：若错误标记在已向客户端吐出正文之后才到，中途切渠道重来可能产生重复内容。针对性缓解为 `RetryOverrideDropDuplicatePreamble`（丢重复的 role preamble）。限流场景通常首个 chunk 即命中并被吞掉，一般不触及。
- **适用范围**：body 拦截位于 openai 适配器（`relay/channel/openai/`），Responses 的流式/非流式处理器均挂了 guard，`openai_responses` 正好覆盖。

## 计费问题的考虑

核心结论：这条 200 兜底链路**不会向用户重复计费**，被丢弃的 200 限流响应也**不计费**，用户最终只为真正成功的那个渠道付费。

### 计费模型：一次预扣、末次结算、失败全退

- **一次预扣**：`PreConsumeBilling` 在 relay 处理入口只执行一次（`controller/relay.go:164`），基于 `prompt tokens + max_tokens 估算`，在 `relayInfo.Billing`（`BillingSession`）上挂一笔预扣。整个重试/兜底循环都发生在这一次 relay 调用内部，**不会按渠道叠加预扣**——兜底到 N 个渠道也只有一笔预扣占用。
- **末次结算**：只有最终成功的那次尝试会算出真实 usage 并调 `SettleBilling(actualQuota)`（`service/billing.go:51`），对预扣做补扣/返还差额（delta = 实际 − 预扣）。
- **失败全退**：若所有渠道耗尽、最终 `newAPIError != nil`，入口 defer（`controller/relay.go:170-179`）调 `Billing.Refund(c)` 把整笔预扣退还（幂等、异步）。用户对彻底失败的请求零付费。

### 被吞掉的 200 尝试不计费

guard 命中后响应处理器 `return nil, retryBodyErr`（`relay_responses.go:159-166`），**不计算 usage、不结算**。代码注释明确写着 "The abandoned attempt's tokens are not billed (original behavior)"。即：那段被丢弃的 200 限流响应，其输入/输出 token 都不向用户收费。

### 需要注意的点

- **上游成本由网关侧承担**：虽然用户不为失败尝试付费，但每次兜底都会把（可能很大的）prompt 真实发给下一个上游，这部分上游调用成本/延迟由网关运营方承担。规则命中越频繁、渠道越多，浪费的上游请求越多——这是运营成本而非用户计费问题。（实际上在responses流式访问时，只要是中途429的情况，最终上游azure并不会为前面已经给出的响应计费，newapi等网关的行为也是不计费）
- **流式已吐出部分内容的情况**：若限流标记在已向客户端流式吐出部分正文之后才到（`responseTextBuilder.Len() > 0` → 置 `RetryContinuationContentSent`），这部分已发 token 同样不计费（丢弃尝试不计费），客户端后续收到兜底续写，重复由 `RetryOverrideDropDuplicatePreamble` 缓解。计费上仍只结算最终成功渠道。
- **预扣额度占用**：预扣是一次性估算值，在整个多跳兜底期间一直占用；若用户余额紧张，较大的预扣可能直接卡在预扣阶段（返回余额不足）。但它是单笔占用，不会 N 倍膨胀。
- **无重复计费风险**：预扣一次 + 成功一次结算，跨兜底跳不产生二次扣费。

## 关键代码位置

- `relay/channel/openai/retry_body_guard.go` — `retryBodyGuardError` / `retryBodyGuardMaxBytes`
- `relay/channel/openai/relay_responses.go`、`chat_via_responses.go` — guard 调用点（流式/非流式）；`relay_responses.go:159-166` 丢弃尝试不计费
- `relay/retryrule/retryrule.go` — `SuccessContext` / `ResponseContext` / `ShouldTriggerOnBody` / `CollectRewrites`（含 200 强制 fallback）
- `controller/retry_fallback.go` — `maybeApplyRetryRuleFallback`（fallback 分支）
- `controller/relay.go:164` 预扣、`:170-179` 失败退款 defer；`service/billing.go` — `PreConsumeBilling` / `SettleBilling`；`service/billing_session.go` — `BillingSession.Refund`（幂等）
