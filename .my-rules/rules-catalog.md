# 参数覆盖 / 重试覆盖规则清单（问题 → 规则）

> 本文档按「出现了什么问题 → 加了什么规则解决」整理当前 `.my-rules/` 下的实际配置，便于回溯每条规则的来由。
> 规则机制与模板化方案见 `docs/override-rule-templates.md`（功能架构），本文只记录具体规则内容。
> param 规则搞不定/有取舍的报错（整块类型不支持、需结构转换等）另见 `.my-rules/hard-to-handle-errors.md`。
>
> 配置来源：
> - 参数覆盖：`.my-rules/my-param-rules-claude.json`（Claude，下表列出 29 条）+ `.my-rules/my-param-rules-gpt.json`（GPT，4 条），下表合并编号共 33 条。注：Claude 文件另有一条"anthropic-beta 黑名单剔除"规则（用户新增）暂未收录进下表。
> - 重试覆盖：`.my-rules/my-retry-rules.json`（6 条）、`.my-rules/my-channel-setting.json` 的 `retry_override`（5 条）
> - `.my-rules/c.json` 是一个抓包的 `/v1/responses` 响应示例，非规则配置。

---

## 一、参数覆盖规则（param_override）

参数覆盖在请求发往上游前改写 body / header，用来适配不同上游（尤其 AWS Bedrock、OpenAI gpt-6）对 Claude / GPT 参数的差异化约束。按主题分组。

### 1. system 字段清洗（Bedrock 对 system 格式敏感）

| # | 问题 | 规则 |
|---|------|------|
| 1 | system 为数组且首段 text 带首尾空白，上游不接受 | `regex_replace` `system.0.text` 去首尾空白（`^\s+\|\s+$` → ``） |
| 2 | system 为数组但首段 text 为空，带空 system 会报错 | 当 `system.0.text == ""` 时 `delete` 整个 `system` |

### 2. Claude thinking / output_config 适配（统一走 adaptive）

Claude 5 系列不再接受 `thinking.type == "enabled"`；opus-4-7 等模型则**只支持 `adaptive`**（`disabled`/`enabled` 均 400），需统一转成 `adaptive` 并补 `output_config.effort`。

| # | 问题 | 规则 |
|---|------|------|
| 3 | `claude-sonnet-5` 传了 `thinking.type=enabled`，需要补 effort | `set` `output_config = {"effort":"medium"}`（cond: `thinking.type==enabled` AND `model` 前缀 `claude-sonnet-5`） |
| 4 | 同上，thinking 本身要转 adaptive | `set` `thinking = {"type":"adaptive"}`（同条件） |
| 5 | `claude-opus-5` 同样问题，补 effort | `set` `output_config = {"effort":"medium"}`（cond: `thinking.type==enabled` AND 前缀 `claude-opus-5`） |
| 6 | 同上，thinking 转 adaptive | `set` `thinking = {"type":"adaptive"}`（同条件） |
| 7 | `claude-opus-4-7` 仅支持 adaptive，`thinking.type=disabled` 会 400（ValidationException: disabled is not supported，要求用 adaptive + output_config.effort）；存在 thinking 且 type 缺失或非 adaptive 时补 effort | `set` `output_config.effort = "medium"`（`keep_origin:true`；cond: 前缀 `claude-opus-4-7` AND thinking 存在 AND `thinking.type != adaptive`（`pass_missing_key`）） |
| 8 | 同上，thinking 统一转 adaptive | `set` `thinking = {"type":"adaptive"}`（同条件） |
| 9 | `claude-opus-4-8`（提前适配，预期同 4-7 仅支持 adaptive）存在 thinking 且 type 缺失或非 adaptive，缺 effort | `set` `output_config.effort = "medium"`（`keep_origin:true`；cond: 前缀 `claude-opus-4-8` AND thinking 存在 AND `thinking.type != adaptive`（`pass_missing_key`）） |
| 10 | 同上，thinking 统一转 adaptive | `set` `thinking = {"type":"adaptive"}`（同条件） |
| 11 | `claude-opus-5`（前缀含 opus-5 / opus-5-5，统一兜底 opus-5 全系；原 opus-5-5 专属规则已删除并入此条）存在 thinking 且 type 缺失或非 adaptive，缺 effort | `set` `output_config.effort = "medium"`（`keep_origin:true`；cond: 前缀 `claude-opus-5` AND thinking 存在 AND `thinking.type != adaptive`（`pass_missing_key`）） |
| 12 | 同上，thinking 统一转 adaptive | `set` `thinking = {"type":"adaptive"}`（同条件） |

### 3. Claude 其它字段兼容（避免 Bedrock 400）

| # | 问题 | 规则 |
|---|------|------|
| 13 | `claude-opus-5` 的 `output_config.format` 不再被支持 | `delete` `output_config.format`（cond: 前缀 `claude-opus-5`） |
| 14 | 工具定义里的 `strict` 不被 Bedrock 支持（报错 `tools.N.custom.strict: Extra inputs are not permitted`，`custom` 是判别联合变体标签 `type=custom`，真实字段在工具顶层 `tools.N.strict`；opus/sonnet 全系都会报，含 sonnet-5-5） | `delete` `tools.*.strict`（cond: 前缀 `claude-`；逐个剥掉，工具与 input_schema 保留，响应仍是 tool_use，对客户端无感） |
| 15 | 工具里的 `defer_loading`（tool-search-tool beta 字段，Claude Code 开启动态工具加载时写入）不被 Bedrock 支持（报错 `tools.N.custom.defer_loading: Extra inputs are not permitted`，其中 `custom` 是判别联合的变体标签 `type=custom`，**真实 JSON 字段在工具顶层** `tools.N.defer_loading`） | `delete` `tools.*.defer_loading`（cond: 前缀 `claude-`；逐个剥掉，工具定义本身保留，对客户端无感） |
| 16 | `thinking.display` 传了非法值（非 `summarized`/`omitted`）导致 Bedrock 400 | `set` `thinking.display = "summarized"`，仅在值既不是 `summarized` 也不是 `omitted` 时兜底（合法值保留） |
| 17 | 请求带 `context_management` 时需启用 Bedrock 服务端上下文管理 | `set_header` `anthropic-beta` `$append` `context-management-2025-06-27`（cond: 前缀 `claude-` AND 存在 `context_management`） |
| 18 | `claude-sonnet-5-5` 关闭 thinking 用 `disabled` 会 Bedrock 400 | `set` `thinking.type = "between_tools"`（cond: 前缀 `claude-sonnet-5-5` AND `thinking.type==disabled`） |
| 19 | `thinking.type==between_tools` 时不允许 `display`，报 400 `thinking.between_tools.display: Extra inputs are not permitted`（`between_tools` 是判别联合变体标签，真实字段 `thinking.display`；#16 的 display 兜底会给它补上） | `delete` `thinking.display`（cond: 前缀 `claude-` AND `thinking.type==between_tools`；须排在 #18 转换之后执行） |
| 20 | `claude-opus-5-5` 不支持强制 tool_choice（`any`/`tool`），仅 `auto`/`none`，报 400 `does not support forced tool_choice; use auto or none` | `set` `tool_choice = {"type":"auto"}`（cond: 前缀 `claude-opus-5-5` AND tool_choice 存在 AND `tool_choice.type != auto` AND `!= none`；改成 auto 而非 none，保留"想用工具"的意图） |
| 21 | Bedrock 要求 `anthropic_version`（值仅 `bedrock-2023-05-31`），缺失时报 400 `anthropic_version: Field required`（多见于透传模式下客户端原生 Claude body 不带该字段） | `set` `anthropic_version = "bedrock-2023-05-31"`（cond: 前缀 `claude-`；`keep_origin` 不覆盖已有值。置于 operations 开头） |

### 4. messages 角色与内容块清洗（Bedrock 角色/content 限制）

| # | 问题 | 规则 |
|---|------|------|
| 22 | messages 里出现 `role==developer`（OpenAI 新角色），Bedrock 只允许 user/assistant/system，报 400 `Unexpected role "developer"` | `regex_replace` `messages.*.role` 把 `^developer$` 改写成 `user`（cond: 前缀 `claude-`；锚定精确匹配，保留该条内容，不误伤其它角色） |
| 23 | thinking 内容块里的 `endpoint_signature` 不被 Bedrock 支持（报错 `...content.M.thinking.endpoint_signature`，`thinking` 是判别联合变体标签 `type=thinking`，真实字段在块顶层 `messages.N.content.M.endpoint_signature`） | `delete` `messages.*.content.*.endpoint_signature`（cond: 前缀 `claude-`；逐个删除，`signature` 本体保留，不影响 thinking 多轮验签） |
| 24 | messages 里混入 `role==system`，Bedrock 不接受 | `prune_objects` `messages`，删除 `where role==system`（`recursive:false`） |
| 25 | content 内有空 text 块（`type==text && text==""`）导致 Bedrock 400 "text content blocks must be non-empty" | `prune_objects` `messages.*.content`，删除 `type==text && text==""`（cond: 前缀 `claude-`；`recursive:false`，避免误入 `tool_use.input` 触发类型比较崩溃） |
| 26 | thinking 块没有 thinking 文本（`type==thinking` 且 `thinking` 为空或缺失）导致 400 `each thinking block must contain thinking` | `prune_objects` `messages.*.content`，删除 `type==thinking && thinking∈{空,缺失}`（cond: 前缀 `claude-`；`recursive:false`，`thinking` 条件带 `pass_missing_key`） |
| 27 | `messages.0.content` 为空数组 `[]` 时 Bedrock 400 non-empty content | `set` `messages.0.content = "."` 占位（cond: 前缀 `claude-` AND `messages.0.content.# == 0`，用长度判空保证类型安全） |
| 28 | `claude-opus-5` 不支持 assistant 消息预填充（prefill），对话须以 user 结尾，报 400 `does not support assistant message prefill` | `delete` `messages.-1`（cond: 前缀 `claude-opus-5` AND `messages.-1.role==assistant`；删掉末条 assistant 使对话落回上一条 user。放在所有 messages 清洗之后；prefill 意图在该模型无法保留） |

### 5. temperature / token 参数约束

| # | 问题 | 规则 |
|---|------|------|
| 29 | Claude 新世代（opus-5 / sonnet-5 及 opus-4-6/4-7/4-8）已弃用 temperature，带上会 400 "temperature is deprecated" | `delete` `temperature`（cond: model `contains` `claude-opus-5` OR `claude-sonnet-5` OR `claude-opus-4-6` OR `claude-opus-4-7` OR `claude-opus-4-8`；按精确型号，不放宽到老的 opus-4/4-1） |
| 30 | `gpt-6` 只支持默认 `temperature=1`，其它值 400 | `delete` `temperature`（cond: 前缀 `gpt-6` AND `temperature != 1`） |
| 31 | `gpt-6` 的 `max_output_tokens` 低于最小值 16 会 400 | `set` `max_output_tokens = 16`（cond: 前缀 `gpt-6` AND `max_output_tokens < 16`） |
| 32 | `gpt-6` 在 chat/completions 下 function tools 与 `reasoning_effort` 冲突 400 | `set` `reasoning_effort = "none"`（cond: 前缀 `gpt-6` AND 存在 `tools` 中 `type==function` AND `reasoning_effort != none`） |
| 33 | `gpt-6.1-sol` 不支持 `reasoning_effort=none`（仅 low/medium/high/xhigh/max），#32 兜底置的 none 或客户端直传 none 都会 400 `'none' is not supported` | `set` `reasoning_effort = "low"`（cond: 前缀 `gpt-6.1-sol` AND `reasoning_effort==none`；置于 #32 之后，兜住自身兜底与客户端直传） |

---

## 二、重试覆盖规则（retry_override）

重试覆盖在上游返回错误时，按「phase1 请求条件 + phase2 响应条件」命中后，改写请求体（phase3）并决定重试动作（phase4：`retry_same_channel` 原渠道重试 / `fallback_next_channel` 兜底下一渠道）。

| # | 问题 | phase1（请求） | phase2（响应命中） | phase3（改写） | phase4 |
|---|------|---------------|-------------------|---------------|--------|
| R1 | Claude 发生兜底时 thinking 块 signature 验证不过 | model 前缀 `claude-opus-5` AND relay_format `claude` | 400 AND error 含 `Invalid 'signature' in 'thinking' block` | 删除 `messages` 中 `type==thinking` 与 `type==redacted_thinking` | 原渠道重试 |
| R2 | GPT 可能 200 响应码的同时限流 | model 前缀 `gpt-` AND relay_format `openai_responses` | 200 AND response_body 含 `exceeded rate limit` | 无 | 兜底下一渠道 |
| R3 | GPT 兜底到下一渠道时无法解密 - case1 | 同 R2 | 400 AND error 含 `encrypted content` | 删除 `input` 中 `type==reasoning` | 原渠道重试 |
| R4 | GPT 兜底到下一渠道时无法解密 - case2 | 同 R2 | 400 AND error 含 `The requested item was created under a different Azure OpenAI resource` | 删除 `input` 中 `type==reasoning` | 原渠道重试 |
| R5 | GPT reasoning 项 content 分段缺少 `encrypted_content`（stateless 加密推理不完整）- case3 | 同 R2 | 400 AND error 含 `encrypted_content` | 删除 `input` 中 `type==reasoning` | 原渠道重试 |
| R6 | Claude 经 AWS Bedrock 兜底时 thinking 块 signature 验证不过（Bedrock 用反引号包裹 signature/thinking） | relay_format `claude` | 400 AND error 含 `` Invalid `signature` in `thinking` block `` | 删除 `messages` 中 `type==thinking` 与 `type==redacted_thinking` | 原渠道重试 |

补充：`my-channel-setting.json` 还设置了 `retry_override_stream_precommit_chunks: 3`（流式场景下预提交的 chunk 数）。

---

## 三、两份重试配置已对齐

以 `my-retry-rules.json`（线上实际生效，用户 2026-10-15 确认）为准，已把 `my-channel-setting.json` 的 `retry_override` 对齐：

- 补齐到 6 条，加入 R6（Claude 经 AWS Bedrock、报错用反引号包裹 signature 的情况）。
- GPT 规则 model 前缀由 `gpt-5.6-sol` 统一改为 `gpt-`（匹配所有 gpt 模型）。
- 保留 `retry_override_stream_precommit_chunks: 3`。

两份文件的 `retry_override` 规则现已一致，后续以 `my-retry-rules.json` 为单一来源维护。

