# Claude `/v1/messages` 流式响应：newapi 不同阶段断连的计费行为

> 适用范围：Claude Messages 协议（`POST /v1/messages`）、`stream: true`。本文梳理 newapi 在"回源上游"与"处理上游流式响应"各阶段断连时的计费结果，附关键代码位点，便于排障与阈值调优。

## 结论先行

- 计费是**两段式**：发请求前按估算输入 token **预扣费**；收尾时要么**结算差额**（多退少补），要么**全额退还**预扣。
- 二选一的开关只有一个：**`ClaudeHelper` 最终返回 error 还是 nil**。
  - 返回 error → `controller` 的 defer 调 `Billing.Refund` **全额退**，不写消费日志。
  - 返回 nil → 走 `PostTextConsumeQuota` **按已拿到的用量计费**并与预扣结算。
- `ClaudeStreamHandler` 只有在 chunk 处理回调里调了 `sr.Stop(err)` 时才返回 error；**超时 / 写超时 / EOF / panic 导致扫描循环自然结束时返回的是 nil（带已累计 usage）**，即照常扣费。
- **反直觉点**：客户端提前断开**不会**让 newapi 停止回源——客户端断开检测在主循环中被注释掉了，扫描协程跑在 `context.Background()` 下。newapi 会继续把上游流读完再正常计费。

## 两段式计费模型

- **预扣费**：`controller/relay.go:147-170`，`service.PreConsumeBilling` 按估算输入 token 建立 `relayInfo.Billing` 会话并扣减。
- **退费（失败路径）**：`controller/relay.go:172-181` 的 `defer`，仅当 `ClaudeHelper` 返回非 nil error 时 `relayInfo.Billing.Refund(c)` 全额退、且不产生消费日志。
- **结算（成功路径）**：`relay/claude_handler.go:291` 内联调用 `service.PostTextConsumeQuota` → `service/billing.go:51-95` 的 `SettleBilling` 计算 `delta = 实际 - 预扣` 并多退少补。
- 注意 `PostTextConsumeQuota` 是在请求协程上**同步内联**执行、不在 defer 里，所以即便客户端已断开它仍会跑。

## 分阶段计费结果

### 阶段一：回源上游（DoRequest / 等响应头）断连

- `adaptor.DoRequest` 报错（`claude_handler.go:268-271`）或上游非 200（`:276-281`）→ `ClaudeHelper` 返回 error。
- 结果：**全额退还预扣，净扣费 = 0，无消费日志**。

### 阶段二：处理上游流式响应阶段断连

**(a) newapi 自身原因主动断连，且无 handler 级错误**（典型：流式超时 `StreamEndReasonTimeout`、写客户端 30s 超时 `streamWriteTimeout`、scanner/ping 协程 panic）：

- `cleanup()` 会 `cancel()` 并 `resp.Body.Close()`（`relay/helper/stream_scanner.go:124-139`），即 newapi 主动掐断与上游的连接。
- 扫描循环结束，`ClaudeStreamHandler` 的 `err == nil` → 返回**已累计的部分 usage** → 走 `PostTextConsumeQuota`。
- 结果：**按已接收的部分/估算用量扣费**，再与预扣差额结算。只有当估算后 `TotalTokens == 0` 才命中 `text_quota.go:393` 的"无法扣费"分支而不扣。

**(b) 流中出现上游 error 事件，或转发 chunk 时触发 `sr.Stop(err)`**：

- `ClaudeStreamHandler` 返回 `(nil, err)` → `ClaudeHelper` 返回 error。
- 结果：**全额退还预扣，净扣费 = 0**。

## 用量解析与断连时的回退

事件顺序（Anthropic 官方）：`message_start` →（`content_block_start` → `content_block_delta`… → `content_block_stop`）\* → `message_delta`\* → `message_stop`，其间散布 `ping`。

- `message_start` 携带 `usage.input_tokens` + `cache_read_input_tokens` + `cache_creation_input_tokens`，`output_tokens` 为很小初值（1~3）；真实输出量在最后的 `message_delta`。
- 解析位点：`service/relayconvert/internal/claude_messages/to_oai_chat_resp.go:339-389`（`message_start` 写入 PromptTokens/缓存字段/UsageSemantic=anthropic；`message_delta` 写入最终 output 并置 `Done=true`）。
- 断连回退：`relay/channel/claude/relay-claude.go:156-174` 的 `HandleStreamFinalResponse`，**只补缺失字段、不整份覆盖**：
  - `CompletionTokens`：`message_delta` 未到（`!Done`）时，用已累计输出文本 `ResponseText2Usage` **估算**。
  - `PromptTokens`：仅当为 0 才回退到请求估算值 `info.GetEstimatePromptTokens()`。

### 三种断连时点的输入计费精度

- **已收到 `message_delta`**：输入、输出均为上游真实值，计费精确。
- **只收到 `message_start`、未到 `message_delta`**：**输入 token（含 cache_read/cache_creation）为上游真实值、精确计费**；输出按已收到文本估算（近似）。
- **连 `message_start` 都没收到**：PromptTokens 回退为请求估算值；此时缓存字段全为 0，**整段输入按普通 input、乘完整 model ratio 计费**，无法体现缓存折扣/差异；输出通常为 0（没有任何 delta）。

## 上游 `message_start` 的时延与资源（供阈值调优参考）

- `message_start` 里能准确给出缓存命中数，说明上游在发出它之前**已完成分词 + 缓存查找判定**；它不是"请求一到即回的空壳 ack"。
- 到达时延由 TTFT 决定，包含：排队/限流（429 退避 5→10→20s、529 过载等 30~60s）、网络 RTT、**prefill（GPU 密集，随输入长度增长；命中缓存可跳过，TTFT 最多降 ~85%）**。
- 社区实测：正常短输入低负载下 TTFT ~300~800ms；但**官方不保证 `message_start` 极短返回**，`ping` 事件的存在正是为长间隙保活。长 prompt / 限流 / 过载时可达数秒甚至更久。

## 运维含义

- `StreamingTimeout` 设置过小时，面对**长 prompt 或被限流**的合法请求，可能在 `message_start`（真实 input_tokens + 缓存信息）到达前就断连，退化成"全部按估算普通 input 计费、丢失缓存折扣"的 (a)/未收 message_start 路径。
- 若要避免"客户端已走还继续消耗上游 token"的浪费，需要重新启用 `stream_scanner.go:296-301` 中被注释掉的客户端断开检测分支（当前为刻意禁用状态，取舍见该处注释）。

## 关键代码位点速查

- 路由：`router/relay-router.go:82-90`（`/messages` → `controller.Relay(c, types.RelayFormatClaude)`）
- 分发：`controller/relay.go:219-228` → `relay.ClaudeHelper`
- 预扣/退费/结算：`controller/relay.go:147-181`、`service/billing.go:20-95`
- Claude 入口与收尾计费：`relay/claude_handler.go:266-293`
- 流式扫描循环与断连语义：`relay/helper/stream_scanner.go`
- 流式用量累计与回退：`relay/channel/claude/relay-claude.go:106-218`、`service/relayconvert/internal/claude_messages/to_oai_chat_resp.go:332-400`
- 计费与消费日志：`service/text_quota.go`（`PostTextConsumeQuota`，含 `TotalTokens==0` 不扣费分支 `:393`）

## 参考来源

- Anthropic 官方 streaming / prompt caching 文档（事件顺序、message_start usage 字段、prefill/TTFT）。
- 社区实测：TTFT ~300~800ms、429/529 退避建议。

