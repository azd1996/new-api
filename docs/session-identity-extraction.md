# 客户级会话身份提取（Session Identity Extraction）设计

状态：P0 已实现（客户配置模型与迁移、共享提取器、廉价短路、渠道亲和接入、metadata 注入、批量试运行 API、回归测试）。P1（管理端 UI）与 P2（指标/全局规则）未实施。

## 1. 背景与动机

当前网关在客户端未提供 `metadata.user_id` 时，会合成一个 Claude Code 形状的会话身份用于上游会话粘滞：

```
seed = model + 稳定 system 前缀(到第一个 ephemeral 断点) + conversation identity
conversation identity = x-claude-code-session-id 头
                      > messages[0].content
                      > clientIP + UserAgent + apiKey
```

该兜底假设请求携带完整多轮历史（`messages[0]` 是会话开场白）。此类客户端把全部上下文放进 system、每轮只发当轮用户消息，导致 `messages[0]` 逐轮变化，session_id 逐轮轮转，上游节点本地缓存"写完读不到"，命中率塌陷。

同时，渠道亲和（channel affinity）目前从 `metadata.user_id` 提取亲和 key；没有该字段的请求不建立绑定，选路退化为加权随机，可能跨渠道/跨上游 key，进一步破坏缓存域。

目标：

1. 允许为个别客户配置自定义会话 ID 提取规则（正则/gjson/header 等），不影响其他客户；
2. 同一个提取结果同时供给**渠道亲和**与**上游 metadata.user_id**；
3. 提供试运行接口：粘贴若干条真实请求体，检查每条提取出的会话 ID 及跨请求稳定性。

## 2. 设计原则

- **廉价短路**：非目标客户的请求最多付出一次内存 map 查找，不触发正则、不解析 body。
- **fail-open**：配置缺失/提取失败/正则不匹配时回退到现有 legacy 逻辑，绝不拒绝请求。
- **客户端优先**：客户端自带的 `metadata.user_id`（真实 Claude Code）永远最高优先。
- **一套提取代码**：生产路径与试运行接口复用同一提取函数，避免口径漂移。
- **可观测**：日志记录来源与指纹，不记录明文会话值。

## 3. 配置模型

### 3.1 存储

挂在现有 `customers` 表上（与 `claude_cache_optimization_json` 同模式），新增 TEXT 列：

```go
// model/customer.go
type Customer struct {
    // ...
    SessionIdentityJSON string                `json:"-" gorm:"column:session_identity_json;type:text"`
    SessionIdentity     *CustomerSessionIdentity `json:"session_identity" gorm:"-"`
}

type CustomerSessionIdentity struct {
    Enabled                 bool                             `json:"enabled"`
    ModelPrefixes           []string                         `json:"model_prefixes"`     // 空 = 全部
    Sources                 []SessionIdentitySource          `json:"sources"`
    Transform               string                           `json:"transform"`          // "", xxhash64, sha1_16
    ValueRegex              string                           `json:"value_regex"`        // 提取值校验
    OnEmpty                 string                           `json:"on_empty"`           // legacy | stable | none，默认 legacy
    DisableFirstMessageFallback bool                         `json:"disable_first_message_fallback"`
    TTLSeconds              int                              `json:"ttl_seconds"`        // 亲和绑定 TTL，默认全局
}
```

客户 ID 来自 `x-bce-aigw-trace-tag` 解码（`common.TraceTagFromContext`），在 distributor 阶段即可获得，早于渠道选择与 metadata 合成。

### 3.2 提取来源（SessionIdentitySource）

```go
type SessionIdentitySource struct {
    Type string `json:"type"` // metadata_user_id | request_header | gjson | regex
    Key  string `json:"key,omitempty"`  // request_header: header 名
    Path string `json:"path,omitempty"` // gjson/regex: gjson 路径，如 system.0.text / metadata.user_id
    // gjson: 当取到的值是 JSON 字符串（如 metadata.user_id 是 Claude Code blob）时，
    // 再解一层并取该字段（如 session_id）。空则不解。
    JSONStringField string `json:"json_string_field,omitempty"`
    // regex: 限定在 Path 指向的文本上执行，禁止扫描整个 body
    Pattern       string `json:"pattern,omitempty"`
    CaptureGroup  int    `json:"capture_group,omitempty"` // 默认 0 = 整体匹配
}
```

支持来源：

| Type | 说明 |
|---|---|
| `metadata_user_id` | body 的 `metadata.user_id`（原始字符串或解出 `session_id` 字段） |
| `request_header` | 指定请求头，如 `x-claude-code-session-id` |
| `gjson` | 任意 body 字段，如 `messages.0.content` |
| `regex` | `pattern` 仅作用于 `path` 指向的字符串，捕获组提取 |

### 3.3 配置示例（单消息 + system 承载上下文的客户）

```json
{
  "enabled": true,
  "model_prefixes": ["claude-"],
  "sources": [
    {"type": "request_header", "key": "x-claude-code-session-id"},
    {"type": "regex", "path": "system.0.text",
     "pattern": "^\\[([^\\]]+)-fixed-\\d+\\]", "capture_group": 1}
  ],
  "transform": "xxhash64",
  "value_regex": "^[A-Za-z0-9_-]{4,64}$",
  "on_empty": "legacy",
  "disable_first_message_fallback": true
}
```

对该客户 6 轮请求，两个来源依次短路（均未命中）后由 regex 提取出 `v54qx6`，transform 后 6 轮一致。

## 4. 提取流水线与廉价短路

提取在 **distributor 中、亲和匹配冻结（`PrepareOriginalRequestChannelAffinity`）之前**执行一次，结果写入 Gin context（`ContextKeyExtractedSessionID`），供亲和与 relay 复用：

```
extractSessionIdentity(customerID, modelName, path, headers, body, apiKey, ip, ua):

  1. body.metadata.user_id 非空?                     -> 返回 (client_metadata, "client")
     // 真实 Claude Code 到此为止，不做任何自定义工作
  2. x-claude-code-session-id 头非空?                 -> 返回 (header, "header")
  3. cfg := GetCustomerSessionIdentity(customerID)    // 内存 map，O(1)
     cfg == nil 或 !cfg.Enabled?                      -> 返回 ("", "legacy")
     // 其他客户到此为止，零 body 解析
  4. cfg.ModelPrefixes 不匹配 modelName?               -> 返回 ("", "legacy")
  5. for source in cfg.Sources:                        // 按配置顺序短路
       v := extract(source)                            // regex 只作用于 source.Path 的字符串
       if v != "" && matches(cfg.ValueRegex):          -> 变换并返回 (v, "custom:<source>")
  6. on_empty 处理:
       legacy: 常规兜底（首条消息 / IP+UA+key）
       stable:  IP + UA + apiKey
       none:    返回空（不注入，不建立亲和）
```

要点：

- **步骤 1/2 对所有请求恒定廉价**；步骤 3 是一次内存查找；正则只在步骤 5 且仅当客户配置存在时执行。
- 正则编译结果全局缓存（复用 `channelAffinityRegexCache` 模式），每个 pattern 只编译一次。
- 提取结果长度上限 256 字符；`transform` 归一化后入缓存 key。
- 每请求只提取一次，结果缓存于 Gin context。

## 5. 接入点

### 5.1 渠道亲和

- 提取发生在 `prepareClaudeMetadataUserIDForRouting` 附近、`PrepareOriginalRequestChannelAffinity` 之前；
- 提取值写入 `ContextKeyExtractedSessionID`；
- 在 `ChannelAffinitySetting.Rules` 的默认 Claude 规则前增加一个内置来源：`{Type: "context_string", Key: "extracted_session_id"}`，或者在规则 KeySources 中显式配置该来源；
- 客户自带 `metadata.user_id` 时仍走现有规则（与提取值一致，优先级不冲突）；
- 亲和缓存 key 仍包含 ruleName + usingGroup + 值，`IncludeModelName` 保持现状。

### 5.2 上游 metadata.user_id

- `DeriveClaudeSessionSeed` 的 conversation identity 优先级调整为：

```
x-claude-code-session-id 头
  > ContextKeyExtractedSessionID（自定义提取或 client metadata 解析结果）
  > messages[0] 启发式（可按客户禁用）
  > clientIP + UserAgent + apiKey
```

- 客户自带 `metadata.user_id` 时网关保持透传，不重新合成（步骤 1 已短路）。
- 透传分支不注入。
- `on_empty: none` 时不注入 session_id（避免错误身份）。

### 5.3 会话分流

behind-cdnaigw 分组的标准/非标分类依据"亲和值是否存在"。自定义提取成功即视为有亲和值，请求按**标准**流量处理；提取失败按现有逻辑分类。分类仍使用请求级冻结快照，重试/覆盖不重新分类。

## 6. 试运行 / 批量请求校验能力

### 6.1 目标

让管理员在配置正式生效前，粘贴若干真实请求体，检查：

1. 每条请求会被提取出什么会话 ID；
2. 同一会话的多轮请求是否得到相同 ID（稳定性）；
3. 提取来源/短路层级是否符合预期；
4. 与 legacy 行为的差异。

### 6.2 API

`POST /api/customer/session_identity/test`（管理员权限）

请求：

```json
{
  "customer_id": "example-customer",
  "config": { "...可选，覆盖客户配置用于试算..." },
  "items": [
    {
      "body": "{\n  \"model\": \"claude-opus-4-6\",\n  \"system\": [...],\n  \"messages\": [...]\n}",
      "headers": {"x-claude-code-session-id": "optional"},
      "api_key": "sk-...123456",
      "client_ip": "1.2.3.4",
      "user_agent": "optional"
    }
  ],
  "expect_same": true
}
```

- `items` 数量上限 20，单条 body 上限 2 MB；
- `body` 必须是合法 JSON（原始字符串），与生产路径解析口径一致；
- `config` 可选：不传则使用客户当前已保存配置，便于"改前/改后"对比；传入则仅试算不落库；
- 服务端不持久化请求体，不打印内容，日志仅记录条数与来源统计。

响应：

```json
{
  "results": [
    {
      "index": 0,
      "ok": true,
      "session_id": "845d08e77d843161",
      "raw_value": "v54qx6",
      "transform": "xxhash64",
      "source": "custom:regex(system.0.text)",
      "short_circuit": "",
      "error": ""
    }
  ],
  "summary": {
    "count": 6,
    "ok_count": 6,
    "distinct_session_ids": 1,
    "session_ids": ["845d08e77d843161"],
    "stable": true,
    "expect_same_met": true,
    "sources_used": {"custom:regex(system.0.text)": 6}
  }
}
```

`source` 取值：`client`（自带 metadata）、`header`、`custom:<type>(<path/key>)`、`legacy:first_message`、`legacy:client_fingerprint`、`none`。同一会话多轮请求 `distinct_session_ids` 应为 1；`expect_same: true` 时在 summary 中断言。

### 6.3 校验语义

- 复用生产提取函数（同一短路顺序、同一变换与校验），保证试算结果即生产行为；
- 逐条独立提取，不因前一条结果影响后一条（无状态）；
- 支持"同一客户不同配置版本"对比：先不带 `config` 调一次，再带候选 `config` 调一次。

### 6.4 UI

客户管理编辑页新增"会话身份"区块：

- 启用开关、模型前缀、来源列表（类型/路径/正则/捕获组/JSON 字段）、transform、value_regex、on_empty；
- "试运行"面板：多行粘贴请求体（每行一个 JSON 或分隔符分隔），展示每条提取结果 + 一致性摘要；
- 保存前强制试运行通过（可选配置项：要求 `expect_same` 满足才允许保存）。

## 7. 可观测性

- 消耗日志记录 `other.session_id`（注入后的最终值指纹）与 `other.session_id_source`，source 扩展为 `custom:<rule>:<source>`；
- 亲和日志已有 `channel_affinity.key_fp`，增加 `identity_source`；
- 指标：按客户统计提取成功/为空/校验失败次数；空值率突增告警（客户端请求形状变化）；
- 不记录明文会话值，仅指纹（复用 `affinityFingerprint`）。

## 8. 性能预算

| 路径 | 成本 |
|---|---|
| 客户端带 metadata.user_id | 现状不变，短路 |
| 无 metadata、无头、客户未配置 | 1 次 map 查找 + 2 次判空 |
| 客户已配置、模型/路径不匹配 | map 查找 + 前缀比较 |
| 客户已配置且匹配 | 至多 N 个 source（上限 5）；regex 作用于单个字段，编译缓存 |
| 试运行 | 离线接口，不进入请求热路径 |

## 9. 变更与回退影响

- **修改提取规则/transform**：session_id 变化，等价于换会话身份。上游缓存按内容前缀命中，若路由节点/上游 key 不变则影响小；若 session 粘滞路由变化，活跃会话约一轮冷启动。
- **关闭配置**：`on_empty` 决定回退行为：
  - `legacy`：回到首条消息启发式。对此类单消息客户端会**逐轮轮转**，命中率塌回低位；
  - `stable`：退到 IP+UA+apiKey，身份稳定，仅一轮冷启动；
  - 建议关闭时先切 `stable` 观察，再删除配置。
- **多实例**：配置经内存缓存 + `SYNC_FREQUENCY` 同步，变更传播期间可能出现短暂混合身份；低峰操作并等待一个同步周期。
- 旧亲和绑定按旧 key 存活至 TTL，不会被命中，无正确性影响（LRU 容量 100k）。

## 10. 测试与验收

1. **回归 fixture**：使用该客户（单消息 + system 承载上下文形态）的 6 条真实请求体，断言：
   - 全部提取出 `v54qx6`（transform 后一致），`distinct_session_ids == 1`；
   - 试运行接口对同一组输入给出相同结论。
2. **零开销断言**：非目标客户/未配置客户的请求，注入计数器断言 regex/JSON 提取函数未被调用。
3. **短路优先级**：客户端带 `metadata.user_id` 时跳过所有自定义来源；带头时跳过正则。
4. **on_empty 行为**：`legacy` / `stable` / `none` 三种回退的 session_id 与注入行为。
5. **亲和复用**：模拟同一客户 6 次选路，断言 6 次命中同一渠道绑定；删除配置后按回退策略行为符合预期。
6. **性能**：对未配置客户与已配置客户分别做基准（提取耗时 < 数百微秒）。
7. **试运行边界**：非法 JSON、超长 body、来源缺失、value_regex 不匹配、`expect_same` 失败。

## 11. 实施阶段

| 阶段 | 内容 |
|---|---|
| P0 | `CustomerSessionIdentity` 模型/迁移、共享提取器（含短路）、接入 `DeriveClaudeSessionSeed`、context 暴露给亲和、试运行 API、6 请求回归测试 |
| P1 | 客户管理 UI 配置 + 试运行面板、i18n（仅 en/zh） |
| P2 | 指标/告警、`stable` 回退策略开关、全局（非客户）规则可选支持 |
