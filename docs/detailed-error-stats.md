# 明细错误统计（Detailed Error Stats）

## 背景

渠道报错（尤其 400）常把真实错误类型淹没在变长的 request-id、时间戳里，日志筛选、按类型统计都很不便。例如同一种 Bedrock `thinking` 签名错误，会因每次不同的 `RequestID` 与 newapi 追加的 `(request id: ...)` 后缀而各不相同，无法聚合。

本特性把「归一化后的错误串」称为**签名（signature）**，并允许管理员为签名配置**简名（short name，即「明细错误类型」）**，从而稳定地统计、检索、并对外暴露指标。

## 核心概念

- **签名**：对错误原文套用一组有序正则规则后的结果，去掉可变 id/时间戳等噪声。长度可变、基数仍可能较大。
- **简名**：管理员为签名配置的类别，**多个签名 → 一个简名**（多对一）；未配置的签名归入「未分类（uncategorized）」。Prometheus 只用简名作标签，基数由管理员控制，不会爆炸。

## 管理员怎么用（典型工作流）

页面提供**两条互补的归并/归类路径**，解决「同一类错误被拆成多行」的问题：

1. **正则规则——让「本是同一错误、只因内嵌可变值不同而分裂」的行自动合并。**
   当你在明细错误里看到两行其实是同一种错误，只因为里面带了不同的**数字串 / id 串 / 时间戳**等可变 token 而被算成不同签名时，就为那段 token 定义一条规则把它掩码掉（例如 `\b\d{5,}\b → *`），它们的签名就会在**写入时**收敛成同一个、合并为一行。
   - 不必纯靠肉眼找：点**「推荐规则」**会扫描当前时间窗内的实际签名，自动识别仍在变化的残留 token（长数字、org/project id、邮箱、ARN、retry-after 等）并给出候选正则，挑选/微调后加入规则集即可。
   - ⚠️ **规则是「写入时」生效、不回溯历史**：新存的规则只影响**之后**产生的错误日志，已入库的旧行签名/指纹不变、不会被重新合并。即「治未来、不治存量」，需等新数据进来才看得到合并效果。

2. **简名 / 错误归因——把「天然不同、但语义同属一类」的签名人工归组。**
   有些错误正则上没法（或不该）合并，但你仍想把它们算作一类——这时用**简名（明细错误类型）**手动把多个签名归到一个类别，用**错误归因**标注责任方（客户端/源站/上游网关等）。这是「人工打标签聚合」，与正则「改串以合并」是不同机制。
   - 简名/归因的重分类在**查询时**解析，对历史数据**立即生效**（与规则不同）；但 Prometheus 的 `newapi_error_total` 是写入时按当时简名累加的计数器，改映射只影响之后的增量。

一句话：**想自动合并重复串用「规则」（治未来），想把不同串归成一类用「简名/归因」（含存量）。**

## 写入时（每条错误日志产出时）

位点：`controller/relay.go` 的 `processChannelError` → `model.RecordErrorLog`。

1. 取**未脱敏原文** `err.ErrorWithStatusCode()`（给请求方的响应仍走 `MaskSensitiveError` 脱敏，两条路径解耦）。
2. `service.NormalizeErrorSignature(raw)` 得到签名；`service.ErrorSignatureHash(sig)` 取 `sha256` 前 32 位 hex 作指纹。
3. 把签名与指纹写入日志既有的 **`other` JSON**（`error_signature` / `error_signature_hash`）——**复用现有可扩展字段，不新增列、不改表结构**，因此对 SQLite/MySQL/PostgreSQL/ClickHouse 全部日志库零 schema 变更。
4. 若 Prometheus 开启，按「当时的简名」给 `newapi_error_total` 计数器 +1。

仅对错误日志（`type=5`）生效；**只对新日志生效**，历史日志无签名。简名映射表 `error_signature_alias` 在**主库**（非日志库，永不为 ClickHouse），走 GORM `AutoMigrate`。

## 归一化规则（纯正则、可配置）

归一化是一组**有序的纯正则替换规则** `{name, pattern, replacement, enabled}`，逐条 `ReplaceAllString`，`replacement` 支持 `$1` 捕获组；Go 用 RE2（线性、无回溯），不怕 ReDoS。

- 默认规则（`service.DefaultErrorSignatureRules`）：压空白、删 `(request id: …)` 后缀、UUID→定长星号常量、长 hex→`*`、按 key 掩 request id（`RequestID:`/`x-amzn-requestid` 等）、ISO 时间戳→`*`。
- 规则集可由管理员编辑并持久化：存为 Option 行 `ErrorSignatureRules`（JSON 数组），启动时在 `InitOptionMap` 后由 `service.ApplyErrorSignatureRulesJSON` 应用，空则保留默认。
- 规则**自上而下顺序执行，顺序有意义**；修改规则只影响之后产生的签名（写时归一化，历史签名不回改）。

## 简名映射

- 模型 `model.ErrorSignatureAlias`：`signature_hash`(唯一) + `signature`(文本备查) + `short_name`。内存缓存 `ResolveErrorShortName`，增删改后刷新。
- 统计页解析简名在**查询时**完成，重分类对历史数据立即生效；Prometheus 计数器在**写入时**按当时简名累加，改映射只影响之后的增量（计数器语义）。

## 下钻

`GetAllLogs` 新增 `error_signature_hash` 过滤参数：因签名/指纹存在 `other` JSON，用跨方言的 `other LIKE '%"error_signature_hash":"<hash>"%'`（复用 `otherFieldLikeCondition`，与 trace-tag 检索同机制）精确匹配。明细错误页「查看日志」跳转「使用日志」并带上该 hash + 时间范围 + `type=5`，复用既有日志列表与详情弹窗。

## 聚合实现

`GET /api/log/error-stats` 在窗口内按过滤条件捞错误日志（上限 `maxErrorStatScanRows=50000` 行），逐行解析 `other` 取签名/指纹，在 Go 内存里按指纹聚合出次数/首末时间/模型·渠道分布。签名存在 `other`（非独立索引列），故**不走 SQL `GROUP BY` 而在内存聚合**——以此换取「零 schema 变更、跨全部日志库（含 ClickHouse）」。因此务必配合时间范围收窄，避免扫描触顶被截断。

## Prometheus 指标

`newapi_error_total{short_name, model, channel, status, protocol, stream, customer}`（`pkg/prom_metrics`）。

- `customer` 当前取 `username`（日后接入真正客户概念时替换取值，标签名不变）。
- 高基数标签（`model`、`customer`）开关：环境变量 `ERROR_METRIC_HIGH_CARD_LABELS`（默认 `true`）；关闭时这两个标签置空值以保持 series schema 稳定。大规模部署（用户/模型多）建议关闭以防 series 基数膨胀。

## 接口清单（均 AdminAuth）

- `GET /api/log/error-stats` — 按签名聚合：签名/简名/次数/首末时间/状态码/模型·渠道 chips。
- `GET /api/error-alias` / `POST /api/error-alias` / `DELETE /api/error-alias/:hash` — 简名 CRUD。
- `GET /api/log/error-signature-rules` / `PUT /api/log/error-signature-rules` — 规则集读取/保存（保存先校验+编译每条正则，非法则 400 且不落库）。
- `GET /api/log/error-signature-rules/suggestions` — 基于窗口内实际签名，推荐可收敛残留可变 token 的候选正则（供复制或加入规则编辑器）。

## 前端

「使用日志」同级新增管理员页「明细错误统计」（`web/default/src/features/error-stats/`）：过滤条、统计表（可内联编辑简名、模型/渠道 chips、下钻）、「编辑归一化规则」弹窗、「推荐规则」弹窗。

## 注意事项

- **错误日志存未脱敏原文**：`Content` 现存上游原始错误，可能含上游密钥片段/内网地址/PII。错误日志（`type=5`）默认**不在** `adminOnlyLogTypes`，普通用户经 `GET /api/log/self` 可见自身错误日志原文。本部署不开放普通用户，故风险可控；**若将来启用普通用户，应将 `LogTypeError` 加入 `adminOnlyLogTypes`，或把原文仅存入 `other.admin_info`、`Content` 保留脱敏版**。
- `error-stats` 聚合在缺省时间窗（start/end 为 0）时会扫描 LOG_DB 错误日志（上限 5 万行后截断、在内存解析 `other` 聚合）；务必配合时间范围使用，大表时考虑给默认窗口。
- 规则集若被清空，签名≈原文，基数会上升；属管理员显式操作。
