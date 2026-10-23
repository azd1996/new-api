# 难处理的上游报错登记（param 规则搞不定 / 有取舍）

> 本文登记那些**无法用 param 覆盖规则干净解决**的 Bedrock/上游报错——要么是整块结构不被支持、要么需要结构转换、要么修复会损失能力。
> 能用 param 规则直接解决的"删字段/改值"类报错见 `rules-catalog.md`；规则机制见 `docs/override-rule-templates.md`。
>
> 适用前提：客户端走 Claude 原生格式经本网关转发到 **AWS Bedrock**，param 覆盖在 `relay/claude_handler.go` 发给上游前作用于请求体。

---

## 先掌握：判别联合变体标签的 path 解读规律

Bedrock 的 Anthropic 接口用 Pydantic 判别联合（discriminated union）按 `type` 校验，报错路径里会**插入变体标签**（即 `type` 的值），它不是真实 JSON 键。读 path 时要把这个标签剥掉、往上一层找真实字段。

已遇到的例子（真实字段 = 报错路径去掉变体标签）：

- `tools.N.custom.defer_loading` → 真实 `tools.N.defer_loading`（`custom` = `type:custom`）
- `tools.N.custom.strict` → 真实 `tools.N.strict`
- `thinking.between_tools.display` → 真实 `thinking.display`（`between_tools` = `type:between_tools`）
- `messages.N.content.M.thinking.endpoint_signature` → 真实 `messages.N.content.M.endpoint_signature`（`thinking` = `type:thinking`）
- `messages.N.content.0.tool_result.content.0` → 定位到 `messages.N.content[0]`(type=tool_result) 的 `content[0]`（`tool_result` = `type:tool_result`）

> 再遇到 `Extra inputs are not permitted` / `Input tag ... does not match` 这类，先按此规律把中间的 `custom/thinking/between_tools/tool_result/...` 当标签剥掉，定位真实字段，再决定能否用 param 规则删/改。

---

## 判断框架：param 规则够用吗？

| 情况 | param 规则能否解决 | 做法 |
|------|-------------------|------|
| 多了个不被支持的**标量字段**（defer_loading、strict、endpoint_signature、between_tools.display 等） | ✅ 能 | `delete` 该字段，见 rules-catalog |
| 某个**角色/枚举值**不被支持，但有等价值可替换（developer→user、disabled→between_tools） | ✅ 能 | `set`/`regex_replace` 改值 |
| **整条对象类型**不被支持（工具 type、内容块 type） | ⚠️ 只能删，会丢能力 | `prune_objects` 删该对象，或换渠道 |
| 需要**结构转换**（字段拆分/重组，如 image_url→image） | ❌ 做不了 | 代码层归一，或降级丢弃 |

param 规则只有 set/delete/copy/move/replace/regex_replace/prune_objects/*_header/sync_fields —— **没有"拆字符串重建嵌套结构"的能力**，凡是需要结构转换的都得走代码层。

---

## 已登记 case

<!-- CASES_PLACEHOLDER -->

### 1. tool_result 里出现 OpenAI 式 `image_url` 图片块

- **报错**：`ValidationException: messages.N.content.0.tool_result.content.0: Input tag 'image_url' found using 'type' does not match any of the expected tags: 'browser_state', 'document', 'image', 'search_result', 'text', 'tool_reference'`
- **模型**：claude-opus-5-5（Bedrock），其它 Claude 同理
- **根因**：客户端把 OpenAI 风格图片块 `{"type":"image_url","image_url":{"url":"..."}}` 放进了 Claude 的 tool_result.content。Anthropic/Bedrock 的图片块是 `{"type":"image","source":{...}}`，**类型名和结构都不同**：
  - data URI：`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"<base64>"}}`
  - https：`{"type":"image","source":{"type":"url","url":"..."}}`
- **为什么 param 规则搞不定**：需要把 `image_url.url`（可能是 `data:image/png;base64,XXXX`）拆出 media_type + base64 数据，再重建 `source` 对象。这是结构转换，param 规则做不到（只有删/改/搬移，不能拆字符串建嵌套结构）。
- **可选处理**：
  - 方案 A（止血、丢图）：`prune_objects` 删掉 `messages.*.content.*.content` 里 `type==image_url` 的子块。请求能过，但该图片丢失，模型看不到。
  - 方案 B（保图、推荐长期）：在网关 Claude 适配层（`relay/channel/claude` 或 compat handler）做 `image_url → image` 格式归一。
- **状态**：待定（看工具返图是否为常规用法再决定走 A 还是 B）。

### 2. 服务端 `web_search_20250305` 工具不被 Bedrock 支持

- **报错**：`tools.0: Input tag 'web_search_20250305' found using 'type' does not match any of the expected tags: 'bash_20250124', 'custom', 'memory_20250818', 'text_editor_*', 'tool_search_tool_*'`
- **模型**：claude-opus-4-6（Bedrock）
- **根因**：客户端（Claude Code）带了 Anthropic 服务端联网搜索工具 `type: web_search_20250305`，Bedrock 该模型不提供此能力。
- **为什么难处理**：整条工具类型不被支持——不是删某个字段能救的。
  - 方案 A（丢能力）：`prune_objects` 删 `tools` 里 `type==web_search_20250305` 的工具，请求能过但没联网搜索；若删空 tools 可能还需删整个 tools 字段。
  - 方案 B（保能力）：把带 web_search 的请求路由到支持的渠道（如 Anthropic 直连），或客户端用自定义工具自己实现搜索。
- **状态**：待定（看是否真需要联网搜索）。

