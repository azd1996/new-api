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

### 3. 工具用了 OpenAI 的 `type: function` 格式

- **报错**：`ValidationException: tools.0: Input tag 'function' found using 'type' does not match any of the expected tags: 'bash_20250124', 'custom', 'memory_20250818', 'text_editor_*', 'tool_search_tool_*'`
- **模型**：Claude（Bedrock），streaming/非 streaming 都会
- **根因**：客户端把 OpenAI 工具格式 `{"type":"function","function":{"name":...,"description":...,"parameters":{...}}}` 发到了 Claude 原生端点。Anthropic/Bedrock 的自定义工具是 `{"type":"custom","name":...,"description":...,"input_schema":{...}}`——类型名、字段层级都不同（OpenAI 把 name/parameters 包在 `function` 里，Anthropic 放顶层且 `parameters` 叫 `input_schema`）。
- **为什么 param 规则搞不定**：需要对每个工具做"嵌套字段上提 + 改名"的结构转换。param 的 `move`/`copy` **只支持单个具体路径、不支持 `tools.*` 通配**（引擎里只有 delete/set/replace/prune 这类走通配展开），工具数量不定无法逐个写死索引。唯一能通配的 `prune_objects` 删 `type==function` 会把**所有工具删光**，等于废掉整个工具调用场景，不可接受。
- **new-api 有没有现成能力？**（已核查，2026-10-22）
  - 转换器**存在但只在 OpenAI 入口触发**：`service/relayconvert/internal/oai_chat/to_claude_messages_req.go:30` 的 `OpenAIChatRequestToClaudeMessages` 会把 `function.name/description/parameters` → `name/description/input_schema`（输出 `{name,description,input_schema}`，不带 `type:custom`，是标准 Anthropic 格式）。它仅在"客户端用 OpenAI chat-completions 格式打到 Claude 渠道"时经 `claude/adaptor.go:95` `ConvertOpenAIRequest` 调用。
  - **原生 Claude 端点（`/v1/messages`, relay_format=claude）是透传**：走 `ConvertClaudeRequest`——claude 适配器 `claude/adaptor.go:28` 直接 return，AWS 适配器 `aws/adaptor.go:41` 只改 messages 图片 source、从不碰 tools。所以 OpenAI 格式 tools 原样透传给 Bedrock → 400。
  - **没有任何开关能强制 tools 归一**：`force_format` 只管把 OpenAI 渠道的**响应**格式化成 OpenAI 标准，与请求 tools 无关。
- **可选处理**：
  - 方案 A（零代码，推荐先试）：客户端改走 new-api 的 **OpenAI 兼容端点 `/v1/chat/completions`** 指向该 Claude 渠道，走现成转换器自动把 tools 转成 Claude 格式。前提是客户端可切端点/格式。
  - 方案 B（要改代码）：在原生 Claude 路径（`aws/adaptor.go:41` 的 `ConvertClaudeRequest`，或 claude 适配器）加工具归一 `{type:function,function:{…}}` → `{name,input_schema}`，不管客户端走哪个端点都兜得住。
- **状态**：暂不处理（2026-10-22 决定）。先不做代码改动；如客户端可切 `/v1/chat/completions` 则零成本绕过。后续若需在原生 Claude 路径兜底再走方案 B。

### 4. 未知内容块 type（如 `weird_block`）不被 Bedrock 接受

- **报错**：`messages.N.content.0: Input tag 'weird_block' found using 'type' does not match any of the expected tags: 'connector_text', 'document', 'image', 'mid_conv_system', 'redacted_thinking', 'search_result', 'server_tool_use', 'text', 'thinking', 'tool_result', 'tool_search_tool_result', 'tool_use'`
- **模型**：Claude（Bedrock）
- **根因**：某条消息的内容块 `type` 不在 Bedrock 允许的内容块类型里。`weird_block` 不是任何已知 Anthropic 类型，疑似占位/测试值或某客户端注入的非标准块。
- **当前允许的内容块类型**（来自报错，供白名单参考）：`connector_text / document / image / mid_conv_system / redacted_thinking / search_result / server_tool_use / text / thinking / tool_result / tool_search_tool_result / tool_use`。
- **为什么难处理**：未知块无从转换（不知道该映射成什么），只能丢弃；而丢弃要么得知道确切 type 字面值，要么维护白名单。
  - 方案 A（精确丢弃）：`prune_objects` 删 `messages.*.content` 里 `type==<确切值>` 的块。仅在该 type 固定且确认可丢时用；为偶发假值硬编码意义不大。
  - 方案 B（未知块清道夫，通用）：invert 条件白名单——删掉 type 不在允许列表里的内容块，一次兜住所有未知 type。代价：白名单需人工维护，Anthropic 新增合法块类型时若没同步会误删；空块/无 type 块的边界需处理。
- **状态**：暂不处理（2026-10-24 决定）。`weird_block` 判定为非真实类型（疑似测试值），线上未确认有真实客户端发送；若后续出现真实未知块再按确切 type 走 A 或按需上 B。

### 5. thinking 块上的 `cache_control` 不被 Bedrock 接受（且不能 broad 删，会伤缓存）

- **报错**：`messages.N.content.M.thinking.cache_control: Extra inputs are not permitted`（`thinking` 是判别联合变体标签 `type=thinking`，真实字段在块顶层 `messages.N.content.M.cache_control`）
- **模型**：Claude（Bedrock），streaming 下出现
- **根因**：某个 thinking 内容块上带了 `cache_control`（客户端把缓存断点加到了 thinking 块），Bedrock 不允许 thinking 块带 cache_control。
- **为什么难处理（不同于 #22 endpoint_signature）**：`endpoint_signature` 只存在于 thinking 块，可以 `messages.*.content.*.endpoint_signature` 一把删、无副作用。但 **`cache_control` 在 text/tool_use/tool_result 等块上是合法的缓存断点**。param 引擎的限制（`relay/common/override.go`）决定了没法只删 thinking 的：
  - `delete` 路径通配只认 `*`，`collectWildcardPaths` 不支持按 `type==thinking` 条件筛，`deleteValue`→`sjson.DeleteBytes` 也不支持 gjson 查询；
  - `conditions` 是整条规则的全局开关，非逐元素。
  - 所以 `messages.*.content.*.cache_control` 会把**所有块的 cache_control 全删** → prompt 缓存断点丢失、命中率下降、成本/延迟上升。
- **可选处理**：
  - 方案 A（param，有损）：全删 `messages.*.content.*.cache_control`。仅当缓存断点**不加在消息内容块**（只在 system/tools 层）时才可接受。
  - 方案 B（代码层，精准，推荐）：在 `aws/adaptor.go` 的 `ConvertClaudeRequest` 遍历 content，**只删 thinking 块上的 cache_control**，其它块保留。不伤缓存。
- **状态**：暂不处理（2026-10-24 决定）。待确认缓存断点的放置层级后，再在 A/B 间取舍（倾向 B）。

### 6. assistant 消息预填充（prefill）不被支持（已处理，记取舍与踩坑）

- **报错**：`This model does not support assistant message prefill. The conversation must end with a user message.`
- **模型**：claude-opus-5（带 extended thinking 的模型普遍不支持 prefill）
- **什么是 prefill**：`messages` 末条是 `role: assistant`、内容为"答案的开头"，让模型从这段开头续写，用来约束输出格式/口吻。例：`[..., {role:user,"给个JSON"}, {role:assistant,"{"}]` 让模型从 `{` 续写得到纯 JSON。带 thinking 的模型要先生成 thinking 再出正文，无法"替它开头"，故拒绝。
- **已采用处理**：rules-catalog #27 —— `delete messages.-1`（cond: 前缀 `claude-opus-5` AND `messages.-1.role==assistant`），删掉末条 assistant 使对话落回上一条 user。放在所有 messages 清洗之后。`processNegativeIndex` 对条件路径和操作路径都生效，故 `messages.-1` 能正确定位当前末条。
- **取舍**：会丢掉 prefill 的"强制开头"约束（在不支持 prefill 的模型上本就无法保留）；删后模型重新回答上一条 user，语义正常、不二次报错。
- **为什么不改成 `role: user`（踩坑记录）**：
  - prefill 前一条通常是 user，把末条 assistant 改成 user 会产生**连续两条 user** → 撞 Bedrock 的"角色必须交替"校验，400 换一个错（本网关透传、不会自动合并同角色消息）。
  - 即使不报错，prefill 片段（如 `"{"`）变成 user 消息，会被模型当成用户输入去回应，语义错误。
  - 仅当 prefill 前一条恰好是 assistant（罕见）时改 user 才合理，不值得特殊处理。
- **遗留边界**：若末尾连续两条 assistant（罕见），#27 只删一条、可能仍报错；正常 `[user, assistant]` 结构一删即正常。

### 7. 最新 assistant 消息的 thinking 块被改动（与 #22/#25 清洗规则冲突）

- **报错**：`messages.N.content.2: `thinking` or `redacted_thinking` blocks in the latest assistant message cannot be modified. These blocks must remain as they were in the original response.`
- **模型**：Claude（Bedrock），多轮工具调用场景（`[..., assistant(thinking+tool_use), user(tool_result)]`，最新 assistant 带 thinking，需逐字节保留）
- **根因/冲突**：我们为解决别的报错加了会**修改 thinking 块、且对所有消息生效**的规则：
  - #22 `delete messages.*.content.*.endpoint_signature`（若最新 assistant 的 thinking 块带 endpoint_signature，会被删 → 构成"改动"）
  - #25 空 thinking 块删除
  - 这与"最新 assistant 的 thinking 不许改"直接矛盾：同一个 endpoint_signature，**带着**报 `Extra inputs are not permitted`（#22 的由来）、**删掉**又报本条"cannot be modified"，方向取决于"是不是最新 assistant 消息"。
- **为什么 param 规则难办**：需要"旧 assistant 消息可清洗 thinking，但最新那条 assistant 的 thinking 一字不动"。当前引擎：路径通配只有 `*`，无法表达"排除最新 assistant"；`messages.-1` 不一定是最新 assistant（其后常跟 user/tool_result）；没有"按角色+是否最新"联合筛选再局部改字段"的能力。
- **可选处理**：
  - 方案 A（代码层）：在适配器里对"最新 assistant 消息"豁免所有 thinking 清洗（endpoint_signature/空块等只作用于更早的历史消息）。
  - 方案 B（扩展 path 语法）：给 param 引擎加谓词/位置选择符（见下方讨论），让 #22/#25 能写成"只作用于非最新 assistant 的 thinking"。
- **状态**：暂不处理（2026-10-24 登记）。待拿到真实报错请求体确认是否确为 #22/#25 改动了最新 assistant 的 thinking；若是，按方案 A 或 B 收窄。

### 8. 图片单边超过 Bedrock 8000px 硬限制（param 彻底无能为力，需代码层降采样）

- **报错**：`ValidationException: image dimensions exceed max allowed size: 8000 pixels`
- **模型**：Claude（Bedrock）多模态，单边 ≤ 8000px 是 Bedrock 对 Anthropic 接口的硬限制；客户端传大图（曾伴随误写模型名如 `claude-opus-4-8`）即触发。
- **根因**：这是**图片像素尺寸**超限，不是 JSON 字段/结构问题。网关当前把图片当不透明 base64 全程透传（唯一解码是 `service/token_counter.go`/`service/image.go` 用 `image.DecodeConfig` 只读宽高做 token 计数），不做任何 resize/re-encode，故原样把超大图发给 Bedrock → 400。
- **为什么 param 规则搞不定**：param 只能删/改/搬移 JSON 字段，**无法缩放或重编码二进制图像**——比"结构转换"更远，根本不在 param 能力范围内。删掉图片块能让请求过，但等于丢图。
- **可选处理**：
  - 方案 A（止血、丢图）：`prune_objects` 删掉超限图片块。请求能过但模型看不到该图。
  - 方案 B（保图、需代码，推荐长期）：网关侧自动降采样。注入点 `relay/channel/aws/adaptor.go` 的 `ConvertClaudeRequest`（已逐条遍历 content、url 源会 fetch 成 base64），在此取 base64 缩图后回写 `source.Data`。推荐"最小可用版"：只处理 base64 的 JPEG/PNG/GIF，超长边阈值（可配，默认 8000 或更激进的 Claude 推荐 1568）用 `golang.org/x/image/draw` 等比缩放重编码；WebP/HEIC 原样透传；带开关 + 像素上限。
    - **难点/风险**：① WebP 无编码器、HEIC stdlib 根本不能解码（项目仅手写 header 解析尺寸），要么引 cgo 要么降级转 JPEG；② 重编码有损、media_type 变更、token 计数需与实际发送一致；③ 热路径 CPU/内存 + **decode bomb 防护**（须像素上限/超时，勿无脑 `image.Decode`）。完整版（WebP 编码 + HEIC 解码依赖）成本陡增，不建议先做。
  - 方案 C（零代码绕过）：把带大图的请求路由到非 Bedrock 渠道（如 Anthropic 直连），或客户端自行缩图后再发。
- **状态**：暂不处理（尚在评估，未确认实施）。详见记忆 `project_bedrock_image_downsample.md`。若推进，走方案 B 的"最小可用版"，先出具体方案（配置项位置/阈值默认/函数落点/decode bomb 防护）再动手。

### 9. AWS 账号未获 Anthropic 模型访问权限

- **报错**：`status_code=400, InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: ..., ValidationException: Access to Anthropic models is not allowed for this account.`
- **模型**：Claude 新旧模型均可能出现，Bedrock streaming/non-streaming 均同理
- **根因**：当前 AWS 账号没有获得 Amazon Bedrock 中 Anthropic 模型的访问权限。请求已到达 Bedrock，但在模型调用前的账号级权限校验阶段被拒绝；这不是 Claude 模型版本、模型 ID 或请求参数问题。
- **为什么 param 规则搞不定**：这是 AWS 账号、Region 和 IAM/Organizations 权限层面的访问控制错误，与请求 JSON 字段无关，删除或修改参数不能解决。
- **排查与处理**：
  - 确认实际使用的 AWS 账号、凭证和 Region，避免把权限开在了其它账号或 Region。
  - 在实际请求 Region 的 Bedrock 控制台检查 **Model access**，确认 Anthropic/Claude 模型访问已启用或获批。
  - 若使用 AWS Organizations 成员账号、权限边界或跨区域 inference profile，检查 SCP、IAM 策略以及目标 Region 的 Anthropic 权限。
  - 必要时重新提交 Anthropic 模型使用场景申请，并完成相关 Marketplace/订阅或首次使用确认。
- **状态**：非网关参数适配问题，需由 AWS 账号管理员开通或恢复 Anthropic 模型访问权限。
