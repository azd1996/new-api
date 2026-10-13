# Claude metadata 会话信息注入(user_id / session_id)

## 背景

调用链路:客户公司员工 → 客户网关 → 本 newapi 网关 → 本公司统一网关 → 上游供应商网关 → AWS Bedrock。

客户反馈缓存命中率低。上游供应商按 `metadata.user_id` + `metadata.session_id` 做**会话黏滞路由**(把同一会话的请求打到同一后端节点,以复用该节点的本地 prompt 缓存)。问题在于:

- 大量客户端(pi、copilot、python 等)根本不发送这些字段;
- Claude Code 虽然发送 `metadata.user_id`,但它把 `device_id` / `account_uuid` / `session_id` 包成一个 **JSON 字符串**塞在 `user_id` 里,顶层并没有 `metadata.session_id`。

> 说明:Anthropic/Bedrock 的 prompt 缓存是"内容寻址"的(前缀内容 + `cache_control` 断点 + model),`user_id` / `session_id` **不参与官方缓存键**。它们只服务于供应商网关的**节点黏滞**;黏滞做对 → 节点本地缓存更容易命中。

本功能在 newapi 这一层,为**缺 `metadata.user_id` 的请求**合成一个仿 Claude Code 的 `metadata.user_id`(其中内嵌派生的 `session_id`)。

> 重要(实测):**不写顶层 `metadata.session_id`** —— Anthropic 的 metadata schema 只允许 `user_id`,顶层加 `session_id` 会被上游拒绝。因此 session 一律**内嵌在 `user_id` blob 内**(与真实 Claude Code 完全一致),由供应商从 blob 解析。

## 调用链路

```mermaid
flowchart LR
    EMP["客户公司员工"] --> CGW["客户公司网关"]
    CGW --> NEWAPI["newapi 网关<br/>(本工作区 · 注入 metadata)"]
    NEWAPI --> UGW["本公司统一网关<br/>(部门→上游一一映射)"]
    UGW --> VGW["上游供应商网关<br/>(按 user_id+session_id 做会话黏滞)"]
    VGW --> BR["AWS Bedrock<br/>(内容寻址 prompt 缓存)"]
```

- newapi 负责在出站 body 上补齐 `metadata`;统一网关只做部门→上游的一一映射,不参与黏滞。
- 供应商网关用 `user_id`+`session_id` 把同一会话路由到同一节点;Bedrock 的 prompt 缓存按内容前缀命中。

## 适用范围

- 仅作用于 Claude 原生 `/v1/messages` 路径(`relay.ClaudeHelper`,`relay/claude_handler.go`)。
- 仅在**非透传**分支生效:若开启全局 `PassThroughRequestEnabled` 或渠道 `PassThroughBodyEnabled`,走原始 body 透传,不注入。
- 注入发生在 `RemoveDisabledFields` 之后、param-override 之前。
- 受开关 `claude.metadata_user_id_fill_enabled` 控制,**默认开**;关闭后整段注入逻辑跳过(见「开关」)。

## 开关

该功能由 Claude 配置项 `claude.metadata_user_id_fill_enabled`(`setting/model_setting/claude.go` 中 `ClaudeSettings.MetadataUserIdFillEnabled`)控制,默认 `true`。

它已通过 `config.GlobalConfig.Register("claude", ...)` 注册到配置系统,因此直接复用现成的 option 接口读写、持久化与重启加载,无需额外前端或迁移:

```
PUT /api/option/
{ "key": "claude.metadata_user_id_fill_enabled", "value": "false" }
```

`ClaudeHelper` 非透传分支里以 `model_setting.GetClaudeSettings().MetadataUserIdFillEnabled` 为前置闸门:为 `false` 时**完全不调用** `fillClaudeMetadataUserID`,出站 body 的 metadata 原样透出;为 `true`(默认)时才进入下方「行为」描述的 user_id 闸门判断。

## 行为

闸门以**客户端是否已带 `metadata.user_id`** 为准:

- **已带 `user_id`(典型:Claude Code)→ 完全不动 metadata**:它自己的 blob 内已含 session,交由供应商解析。
- **未带 `user_id` → 合成一个仿 Claude Code 的 `metadata.user_id` blob**:
  - `device_id` = API key 末 6 位;
  - `session_id` = 派生的会话 hash(见「session_id 派生」);
  - `account_uuid` = 空;
  - **session 只放在 blob 内,不写顶层 `metadata.session_id`**(实测顶层该键会被上游拒绝)。

由单个函数 `fillClaudeMetadataUserID` 完成(内部计算并内嵌 session_id)。

## 注入流程图

```mermaid
flowchart TD
    S["jsonData(RemoveDisabledFields 之后)"] --> Q0{"开关 claude.metadata_user_id_fill_enabled?"}
    Q0 -- "否" --> OUT["metadata 原样<br/>→ param-override → 送上游"]
    Q0 -- "是(默认)" --> Q1{"metadata.user_id 非空?"}
    Q1 -- "是(如 Claude Code)" --> OUT
    Q1 -- "否" --> D1["取会话标识(优先级):<br/>1. header x-claude-code-session-id<br/>2. ephemeral cache_control 内容<br/>3. clientIP+UA+apiKey 兜底"]
    D1 --> D2["seed = model ␟ 稳定system前缀 ␟ 会话标识<br/>→ xxhash64 → 16位hex = sessionID"]
    D2 --> U["合成 metadata.user_id blob<br/>device_id=key末6位 · session_id=sessionID · account_uuid=空<br/>(不写顶层 metadata.session_id)"]
    U --> OUT
```

对应 `ClaudeHelper` 非透传分支里,先判开关再调用 `fillClaudeMetadataUserID`,以 `metadata.user_id` 是否存在为闸门。

## session_id 派生

种子 = `model` ␟ `稳定 system 前缀` ␟ `会话标识`(`␟` 为 `\x1f` 分隔符),再 `xxhash64` → 16 位十六进制(`fmt.Sprintf("%016x", xxhash.Sum64String(seed))`)。三段皆空时返回空、跳过写入。

- **model**:取请求体 `model`。换模型 → 种子变 → session_id 变,与"换模型必丢缓存"对齐。
- **稳定 system 前缀(`claudeStableSystemPrefix`)**:取 system 数组中到**第一个带 `cache_control:{type:"ephemeral"}` 断点为止(含该块)** 的文本;无断点时回退首个 block;string 型 system 用全文。断点之后的**动态尾部被排除**,避免尾部变动翻转 session。
- **会话标识(`claudeConversationSeed`,优先级从高到低)**:
  1. 请求头 `x-claude-code-session-id`;
  2. 带 ephemeral `cache_control` 的 system/消息块文本拼接(`claudeCacheControlSeed`);
  3. 兜底 `clientIP + User-Agent + apiKey`(`claudeFallbackSessionSeed`,**不含任何提示词文本**,保证多轮稳定)。

## 关键实现位置

- `relay/claude_handler.go`
  - `fillClaudeMetadataUserID`(单一入口;内部派生并内嵌 session_id)
  - `deriveClaudeSessionSeed` / `claudeConversationSeed` / `claudeStableSystemPrefix` / `claudeCacheControlSeed` / `claudeFallbackSessionSeed` / `claudeSessionHash`
  - `claudeMetadataUserID` 结构体(`device_id` / `account_uuid` / `session_id`)、`claudeDerivedDeviceIDLen = 6`
  - 调用点在 `ClaudeHelper` 的非透传分支,前置判断 `MetadataUserIdFillEnabled` 开关
- 开关配置:`setting/model_setting/claude.go` 的 `ClaudeSettings.MetadataUserIdFillEnabled`(json `metadata_user_id_fill_enabled`,默认 `true`),复用 option 接口
- API key 来源:上下文 `constant.ContextKeyTokenKey`(= `token.Key`,已去除 `sk-` 前缀)
- 依赖:`github.com/cespare/xxhash/v2`(go.sum 已存在;如需把 go.mod 的 `// indirect` 清理掉可执行 `go mod tidy`)

## 边界与注意事项

- **顶层 `metadata.session_id` 一律不写**:实测上游会拒绝该键(Anthropic metadata 只允许 `user_id`)。session 一律放在 `user_id` blob 内;供应商需从 blob 解析 session_id(与真实 Claude Code 一致)。
- **真实 Claude Code 完全不被改动**(它已带 user_id)。
- **`device_id` = key 末 6 位是部门级**、低熵:两个 key 末 6 位相同会跨部门撞号。如介意可改用 `token_id` 或 key 的 hash 前缀(仅换派生来源,闸门不变)。
- **兜底粒度较粗**:同一 `clientIP + UA + apiKey` 会被归为一个 session(多个真实会话合并);但保证稳定、不逐轮漂移。
- 只在客户端缺 `user_id` 时合成 blob;客户端已有的 user_id 原样保留。

## 测试

`relay/claude_handler_metadata_test.go`(表驱动,`testify`):

- `TestFillClaudeMetadataUserID`:无 metadata 合成 blob(device_id=末6位、内嵌 16 位 session_id、**无顶层 `metadata.session_id`**)、客户端 user_id 原样保留、空 user_id 视为缺失、key 过短/为空不改。
- `TestClaudeSessionDerivation`(断言 blob 内嵌 session_id):header 优先且与 model + 稳定前缀组合、cache_control 回退、兜底跨轮稳定、换模型改变、system 尾部变化不改变。

运行:`go test ./relay/ -run 'TestFillClaudeMetadataUserID|TestClaudeSessionDerivation'`。

