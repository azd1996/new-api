# 下游网关 Trace-Tag 日志记录与检索（Downstream Trace-Tag Logging）

## 背景与链路

实际调用链路为：

```
客户 → 下游网关 → 本层 new-api → 上游网关
```

下游网关在转发到本层时，会在请求头 `x-bce-aigw-trace-tag` 里带上一个经确定性认证加密后的 base32 值；解密后可还原出两样东西：

- **下游网关的 request_id**（下游侧单次请求标识，UUID）
- **客户 ID**（即下游侧的 `api_key_id`，可为空＝下游公开模式）

本层把这三样（原始 tag 值 + 客户 ID + 下游 request_id）落到**模型访问的消耗日志（type=2）与错误日志（type=5）**，在日志详情里展示（管理员可见），并支持按这三者检索，用于与下游/上游对账、按客户排障。

> 编码规范与参考实现来自下游网关的设计文档（内部如流知识库）。本层只做**解密**（接收方），不做编码。

## 编码与解密

下游侧构造（本层需严格对齐才能通过认证）：

```
plaintext  = request_id + "|" + api_key_id
macKey, encKey = HKDF-SHA256(trace_tag_key) 派生的两把 32 字节子密钥
siv        = HMAC-SHA256(macKey, plaintext)[:16]
blob       = siv ‖ AES-256-CTR(encKey, iv = siv, plaintext)
header 值  = base32.StdEncoding.WithPadding(NoPadding).EncodeToString(blob)
```

本层解密（`common/trace_tag.go` 的 `TraceTagKeys.Decode`）：

1. 去首尾空白、转大写后 base32 解码（容忍链路上的大小写归一化）
2. `siv = blob[:16]`，`ciphertext = blob[16:]`
3. `plaintext = AES-256-CTR(encKey, iv=siv, ciphertext)`
4. 用 `hmac.Equal(siv, HMAC-SHA256(macKey, plaintext)[:16])` 常量时间校验——**认证失败即拒绝**，不会把被篡改/截断的值静默解成错误 ID
5. 按 `|` 切分，第 1 段＝下游 request_id，第 2 段＝客户 ID（多出的段忽略，便于下游未来追加字段）

子密钥派生：`crypto/hkdf` 的 `hkdf.Key(sha256.New, master, nil, "aigw trace-tag mac" / "aigw trace-tag enc", 32)`（salt=nil，两次独立 Expand，info 标签固定）。

## 密钥配置

解密主密钥通过**环境变量**提供：

- 变量名：`AIGW_TRACE_TAG_KEY`
- 取值：**64 位十六进制**（32 字节），需与下游网关的 `trace_tag_key` 一致

启动时用 `sync.Once` 读取并派生一次子密钥、常驻内存。**未配置或格式非法**时：打一次 `SysError` 并禁用解码（后续只记录原始 tag，不解出客户 ID/下游 request_id），**不影响服务启动**。

## 落库字段（日志 `other` JSON，零迁移、不加列）

解密成功时写入（存于 `other` 顶层）：

- `downstream_trace_tag`：原始 header 值
- `customer_id`：客户 ID（下游 `api_key_id`）
- `downstream_request_id`：下游网关 request_id

注入点在 `model/log.go` 的 `attachDownstreamTraceTag(c, other)`，由 `RecordConsumeLog`（type=2）与 `RecordErrorLog`（type=5）调用。行为：

- header 不存在 → 什么都不记
- header 存在但解密/认证失败 → **只记录 `downstream_trace_tag` 原值**（便于排查），客户 ID/下游 request_id 留空
- `c`/`c.Request` 为空时安全跳过，绝不影响日志写入

## 检索

通用日志筛选支持三个新条件（管理端 `GetAllLogs` 与自助 `GetUserLogs` 均支持）：

- query 参数：`customer_id` / `downstream_request_id` / `downstream_trace_tag`
- 后端 `model/log.go` 的 `otherFieldLikeCondition` + `applyTraceTagFilters` 对 `other` JSON 做 `"key":"value"` 形式的 `LIKE` 匹配，跨方言（SQLite/MySQL/PostgreSQL 用 `ESCAPE '!'`，ClickHouse 用反斜杠），用户输入的 LIKE 元字符会被转义

> 注意：`other` 列无索引，LIKE 为全表扫描——检索时请**配合时间范围等条件收窄**。

## 前端

- 日志详情弹窗：在「会话 ID」下方新增 3 行（**仅管理员可见**：客户 ID / 下游请求 ID / 下游 Trace Tag），存在才展示
- 筛选栏「高级筛选」区：新增 3 个管理员专用输入框，接线复刻既有 `requestId`/`upstreamRequestId` 模式（route search schema → `lib/filter.ts` → `lib/utils.ts` 映射为 snake_case query → `types.ts`）
- i18n key：`Customer ID` / `Downstream Request ID` / `Downstream Trace Tag`，已补齐 en/zh/zh-TW/fr/ru/ja/vi

## 行为与边界

- 公开模式（下游未带 `api_key_id`）：明文为 `request_id|`，`customer_id` 解出为空字符串，属正常
- 认证失败（密钥不符、值被截断/篡改）：丢弃解码结果，仅保留原始 tag
- 这三个字段目前放在 `other` 顶层，`GetUserLogs` 会随 payload 返回给非管理员（UI 已按 `isAdmin` 隐藏）；本部署内 user≈下游网关账号、为其自身请求数据。若需服务端强隔离，可改嵌到 `other.admin_info`（非管理员视图会自动剥离）

## 相关文件

- `common/trace_tag.go`、`common/trace_tag_test.go`（解码 + 密钥派生 + 用文档已知向量的回归测试）
- `model/log.go`（`attachDownstreamTraceTag` / `otherFieldLikeCondition` / `applyTraceTagFilters` / `GetAllLogs` / `GetUserLogs`）
- `controller/log.go`（query 参数透传）
- `web/default/src/features/usage-logs/`（详情弹窗、筛选栏、类型、query 映射）与 `web/default/src/routes/_authenticated/usage-logs/$section.tsx`
