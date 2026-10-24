# 更新日志 / Changelog

本文件记录 new-api 网关（本 fork，分支 `feature/custom-query-api`）在上游之上的自定义变更。
格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。详细设计见 `docs/` 下对应文档。

> 说明：本文件在前端构建时被打包进「系统设置 → 运维 → 维护」页。修改后需重新执行 `bun run build` 才会在页面生效。

## [2026-10-24]

### 新增 Added
- **数据看板金额口径扩展：消耗分布/分流/用户统计可切「渠道折后金额·客户折后金额」（Dashboard Stat-Amount Calibers，管理员）**：把每条消耗日志算出的两个「仅统计」金额（渠道分组倍率折后 `stat_quota`、客户折扣后 `customer_stat_quota`）汇总进数据看板，在三处提供口径切换（均**仅管理员**，总额度口径对所有人保留）。(1) **模型调用分析 → 消耗分布图**新增「总额度/渠道折后金额/客户折后金额」口径切换，并在统计卡下方新增两张管理员金额卡；(2) **分流图**「流量宽度口径」在「按额度/按 Token/按请求数」后新增「按渠道折后金额/按客户折后金额」；(3) **用户统计**新增「按额度/按客户折后金额」切换。数据层：`QuotaData`（`quota_data` 表）新增 `channel_stat_quota`/`customer_stat_quota` 两列（主库 `AutoMigrate` 自动补列），`RecordConsumeLog` 结算时随 `LogQuotaData` 落入，`GetAllQuotaDates`/`GetQuotaDataBy*`/`GetFlowQuotaData`/`GetQuotaDataGroupByUser` 的聚合 `SUM` 带出。前端 `processChartData`/`processUserChartData`/分流 `rowMetrics`+`metricValue` 统一加「口径」参数取值。注意：两列为新加，**上线前的历史 `quota_data` 桶为 0**，切到两折后口径时历史区间偏小/为 0，总额度口径不受影响。
- **明细错误新增「解释」列（Per-signature Explanation/Note）**：明细错误统计页「围绕明细错误」视图新增管理员可内联编辑的「解释」列——一段**按签名**的短文本备注，用来补充「为什么这样标注（简名/归因）」。短文本截断展示、**鼠标悬浮 tooltip 看全文**。按签名哈希与简名/归因同存于别名表（新增 `note` 列 `varchar(255) default ''`，零迁移沿用现有表、随同一缓存与多实例周期同步生效），`/api/error-alias` upsert 支持 `note`，三者（简名/归因/解释）至少其一非空，全清空则删除该行。

### 优化 Changed
- **消耗日志「已应用参数覆盖」(`other.po`) 按数组下标折叠缩略（Param-Override Audit Line Folding）**：同一条参数覆盖规则命中数组多处时，原本会按每个下标各记一行（如 `regex_replace messages.0.role …` 一直到 `messages.55.role`，几十上百行几乎相同的摘要），既撑大消费日志 `other.po`，也拉长后端 `record consume log` 文本与 DB 存储。现在在 `relay/common/override.go` 落库前（`info.ParamOverrideAudit` 赋值处）用新增的 `foldParamOverrideAuditLines` 折叠：把「除数字数组下标外完全相同」的行用区间记法合并，如 `regex_replace messages.[1-9,11,15].role from ^developer$ to user`、连续区间 `messages.[0-55].role`。**多个下标位置同时变化**（如 `delete messages.M.content.N.endpoint_signature`）按维度逐层折叠（内层优先、逐轮迭代直到收敛），且**无损**——绝不臆造未真实出现的下标组合：例如 `messages.{0:[0-2], 1/3/7:[0-1], 2/4/5/6:[0]}` 折成 `messages.0.content.[0-2]` / `messages.[1,3,7].content.[0-1]` / `messages.[2,4-6].content.0` 三行，而非错误的笛卡尔积 `messages.[0-7].content.[0-2]`；对角集（`messages.0.content.0` + `messages.1.content.1`）因无法无损合并而原样保留。无数字下标的行原样透传。前端 `parseAuditLine`（按首个空格切分 action/content）对折叠串完全兼容，**前端零改动**；仅影响之后新写入的日志，旧日志不变。
- **归一化默认规则新增 `vendor-prefixed-id`**：默认规则集加一条通用规则 `\b(toolu|rs|resp|msg|item|fc|call|chatcmpl|req)_[A-Za-z0-9_]{6,}` → `${1}_*`，把带下划线前缀的厂商资源 id（如 Anthropic `toolu_bdrk_…`、OpenAI Responses `rs_…`）收敛、保留前缀。置于 `uuid` 之后、`long-hex` 之前（裸 hex 规则因 `\b` 边界抓不到 `前缀_hex`，故需单独处理）。仅影响**无自定义规则集**的部署与之后新产生的签名。
- **归一化默认规则新增 `encrypted-content-blob`**：默认规则集再加一条 `(encrypted content ).*?( could not be verified)` → `${1}*${2}`，把 OpenAI「加密内容无法校验」错误里 `The encrypted content … could not be verified` 中间的变长 blob（含上游网关 `头...尾` 截断形态、不可靠的 `gAAA` 前缀、`for item rs_…` 等任意内部形态）整体收敛为一行。置于 `strip-newapi-request-id` 之后、`uuid` 之前。仅影响无自定义规则集的部署与之后新产生的签名。
- **明细错误下钻改用 `error_signature_hash` 直列过滤**：既然错误日志已把归一化指纹双写进 `logs.error_signature_hash` 真列，「明细错误」下钻到使用日志的过滤从 `other` JSON 的跨方言 `LIKE` 改为 `logs.error_signature_hash = ?`（走索引、更快）。注意：**列加上之前产生的历史错误日志**仅在 `other` 里有指纹、列为空，用列过滤查不到它们；新日志不受影响。
- **简名/错误归因下拉候选改为全量已配置值**：明细错误页编辑「明细错误类型/错误归因」的下拉候选，此前只取**当前列表**里出现过的值，导致本次结果里没出现的旧配置值选不到。现额外拉取 `GET /api/error-alias` 全量别名，候选为「(归因先放 4 个种子) → 所有已配置别名值 → 当前行额外值」去重合并；增删别名后一并失效 `error-aliases` 查询，新值即时进下拉。
- **明细错误页工具栏布局调整**：三个「围绕…」视图切换按钮移到表格上方同一行的左侧，「推荐规则」「编辑归一化规则」并排置于该行右侧；上方仅保留筛选栏。
- **侧边栏「明细错误统计」改名为「明细错误」**：侧边导航项复用已有 `Detailed Error` 文案（zh=明细错误），缩短占位；页面标题仍为「明细错误统计」。
- 通用日志筛选主行改用 24 列栅格，使时间范围与类型两项都能完整容纳（`common-logs-filter-bar`）。
- **日志 `other` 字段「提列」首例：`error_signature_hash` 真列 + `other` 双写（Log other→column Dual-Write）**：把错误日志的归一化指纹从「仅存 `other.error_signature_hash`」改为**本机 `logs` 表真列 + 保留 `other` 双写**，为后续按列高效过滤/分组打基础。`model.Log` 新增 `ErrorSignatureHash`(`varchar(64)`，仅错误日志有值)，`RecordErrorLog` 在保留 `other` 那份的同时写入新列；本机 ClickHouse 建表 DDL 加列并在 `migrateClickHouseLogDB` 用幂等 `ALTER TABLE logs ADD COLUMN IF NOT EXISTS` 给存量实例补列（先迁列后写值），MySQL/SQLite 走 `AutoMigrate` 自动补列。**读路径不动**（`error_stats`、`GetAllLogs` 的 `error_signature_hash` 过滤仍解析 `other`），故新老行都正常、无需 backfill 或双读兼容查询。**决策：提列后不从 `other` 移除**——提列字段都很短，真列与 `other` 双份开销很小；留在 `other` 即无缝随 `logshipper.Row` 的 `other` 继续同步到远端 ClickHouse 集群，**远端表零改动**、不触碰 ck-ingest/Flink 链路。`logshipper.Row` 未加列。此策略适用于后续所有 `other`→真列的字段提升。详见 `docs/log-field-columnization.md`。
- **原始/折扣金额 5 字段也提列（Stat Amounts → Columns，消耗日志）**：沿用同款双写，把结算时算出的「仅统计」金额从 `other` 提升为本机 `logs` 真列——`RawQuota`/`StatQuota`/`CustomerStatQuota`(CH `Int32`)与 `StatGroupRatio`/`CustomerDiscount`(CH `Float64`)，全部 `DEFAULT 0`。`RecordConsumeLog` 从 `params.Other` 取 `raw_quota`/`stat_quota`/`stat_group_ratio`/`customer_stat_quota`/`customer_discount` 写入对应列并**保留 `other`**（缺键读取得零值，`customer_*` 仅带客户 id 的请求才有）；CH 存量实例由 `migrateClickHouseLogDB` 的 `ADD COLUMN IF NOT EXISTS` 列表循环补齐，MySQL/SQLite 靠 `AutoMigrate`。读路径不动（金额统计 `SumLogStatAmounts` 与详情弹窗仍读 `other`），`logshipper.Row` 未加列、远端零改动。详见 `docs/log-field-columnization.md`。
- **使用日志金额统计改用直列 `SUM`（Stat Amounts Read via Columns）**：既然金额已双写进真列，`SumLogStatAmounts` 从「扫 `other` 并 Go 累加（10 万行上限）」改为 DB 直接 `SUM` 列——`COALESCE(SUM(stat_quota),0)` / `SUM(customer_stat_quota)`，利润用 `SUM(CASE WHEN customer_discount>0 THEN customer_stat_quota-stat_quota ELSE 0 END)`（以 `customer_discount>0` 作「有客户」判据，等价旧逻辑按 `customer_stat_quota` 键是否存在）。去掉 10 万行扫描上限、改为全量聚合，跨 SQLite/MySQL/PG/ClickHouse 通用。注意：**列加上之前的历史消耗日志**金额只在 `other`、列为空(0)，按列 `SUM` 会把它们计 0；新日志不受影响。详情弹窗仍读 `other`。

### 修复 Fixed
- **归一化规则集多实例不同步（Error Signature Rules Cross-Instance Sync）**：归一化规则与普通 K-V 配置不同——它被**编译进进程内缓存**（`service.signatureRules`），仅在启动时和「保存规则的那台实例」的 HTTP handler 里重新编译；其他实例虽经 `SyncOptions` 把新规则值轮询进 `OptionMap`，却从不重新编译，于是一直用旧规则。表现为：保存的那台产出新签名（如 `content.*`），其他实例仍产出旧签名（如 `content.8`），同一类错误并存两种签名。修复：`service.SyncErrorSignatureRules(frequency)` 按 `SYNC_FREQUENCY` 轮询 `OptionMap` 的 `ErrorSignatureRules`，**值变化时才**重新 `ApplyErrorSignatureRulesJSON`（带变更守卫避免无谓重编译；option 为空则回落默认规则），在 `main.go` 与 `SyncErrorAliasCache` 等并列启动。重启后在任一实例改规则，其他实例约 2×`SYNC_FREQUENCY` 内自动跟上，不再需逐台重启。
- **数据看板 `quota_data` 落库吞错且谎报成功（Dashboard Quota Data Silent Write Failure）**：`SaveQuotaDataCache`（`model/usedata.go`）此前把 `Create` 插入错误完全丢弃，且无论成功与否都恒打印「保存数据看板数据成功，共保存 N 条」（N 为缓存条数），导致 `quota_data` 真写不进去时在日志里完全隐身——数据看板/分流/模型调用分析静默无新数据，却看不到任何报错（本次排障一度据此误判）。现改为：`Create`、`increaseQuotaData` 的错误均检查并以 `common.SysError` 记录；`First` 查询区分 `ErrRecordNotFound`（正常「新小时桶」→插入）与真实 DB 错误（此时行是否存在未知，贸然 `Create` 可能重复计数，故记录并跳过本条）；结尾按**实际成功条数**记日志，一旦有失败改用 `SysError` 打印「成功 X 条，失败 Y 条（共 Z 条）」。`increaseQuotaData` 签名由无返回值改为返回 `error`。**仅提升可观测性**，不改写入/去重语义，也未加重试（失败条目增量仍随本轮缓存清空丢弃，与原行为一致）。

## [2026-10-23]

### 新增 Added
- **系统行为页补齐三项运维开关前端（System Behavior Options Frontend）**：为此前仅有后端的三个选项补齐「系统设置 → 运维 → 系统行为」前端——「调试模式」(`DebugEnabled`，即时生效无需重启)、「强制 1h 提示缓存 TTL」(`extended_cache_ttl_setting.rules`，Claude，按 分组×模型 规则命中，留空关闭)、「禁用元数据注入」(`metadata_injection_disable_setting.rules`，按 分组×模型 规则禁用 Claude `metadata.user_id/session_id` 注入，留空则全量注入)。两条规则用「分组 + 模型（逗号分隔、`*` 通配）」行编辑器维护，保存统一走 `/api/option/`，`.rules` 以 JSON 字符串下发；后端逻辑不变。
- **客户配置与错误简名映射的多实例缓存周期同步（Cache Sync Across Instances）**：`main.go` 启动 `SyncCustomerCache`/`SyncErrorAliasCache` 两个 goroutine，按 `SYNC_FREQUENCY` 从 DB 轮询重建「客户映射/折扣」与「错误签名→简名」内存缓存（对齐 `SyncOptions`/`SyncChannelCache`），使在另一实例上的新增/修改能在一个同步周期内传播到本实例，不再仅在处理写操作的那个实例生效；`frequency<=0` 则禁用。
- **错误归因维度（Error Attribution）**：在「明细错误」基础上新增与「简名(short_name)」平级的管理员可配置维度「错误归因(attribution)」——按签名哈希映射，`/api/error-alias` upsert 支持同时写 `short_name`/`attribution`（二者至少其一），缓存 `errorAliasEntry{ShortName,Attribution}` 由 `ResolveErrorAttribution` 读取；明细错误统计页新增按「错误归因」分组模式(`group_by=attribution`)与可内联编辑的归因列。零迁移沿用现有别名表（新增 `attribution` 列，`default:''`）。
- **使用日志用量统计区新增三项金额（管理员）+ Prometheus 金额指标**：通用日志统计条在「用量/RPM/TPM」右侧新增「渠道折后金额/客户折扣金额/利润」三项（仅管理员「全部」视图、受敏感信息掩码控制、响应全部筛选条件）。因三项金额只存在每条日志 `other` JSON、无可 SUM 列，后端 `SumLogStatAmounts` 按与列表一致的筛选扫描匹配日志并在 Go 累加（10 万行上限保护，复用抽出的 `applyAdminLogFilters`）；`/api/log/stat` 返回 `channel_stat_quota`/`customer_stat_quota`/`profit_quota`。同时新增 Prometheus 指标（quota 单位，可除以 `newapi_quota_per_unit` 得美元）：`newapi_channel_stat_quota_total`、`newapi_customer_stat_quota_total`（Counter）与 `newapi_profit_quota`（Gauge，利润可为负故用 Gauge、重启清零），均带 `{model,group,channel}`，在结算点落、不影响实际计费。

### 优化 Changed
- **系统行为页危险开关增加前端防护**：「演示站点模式」「自用模式」禁止开启——尝试开启被拦截并弹浮窗提示；「即使未配金额也允许模型访问」关闭时弹二次确认，提示关闭后未配价模型请求将被拒绝、在用的未定价模型会立即不可用的风险。
- **客户折扣改为按模型系列（前缀）配置（Customer Series/Prefix Discounts）**：把原先「按精确模型名」的客户折扣改为「按模型名前缀」的一组**系列折扣**（如 `claude` / `gpt` / `gemini` / `grok`），并去掉精确模型折扣；取折扣时按**命中模型名的最长前缀**生效，无命中则回退客户级默认折扣 `default_discount`，仍为“仅统计不改扣费”的乘数。客户页列表「系列折扣」列按前缀字符串排序**默认平铺展示**（内容过长截断、悬浮看全文），编辑抽屉改为「前缀 + 折扣」动态行。数据层新增 `prefix_discounts`（TEXT，JSON `前缀→折扣`）；旧列 `model_discounts` 采用**保留悬空列**策略——代码已彻底不再引用，不做重命名/复用/DROP（因 `AutoMigrate` 无法表达列重命名、跨库 `RENAME COLUMN` 语法不一致、且新旧语义不同），跨三种库零风险。`default_discount` 列不变。详见 `docs/customer-management-and-discount.md`。
- **通用日志筛选：客户名称/客户ID 改为下拉 + 布局调整（Log Filter Customer Dropdowns）**：管理员通用日志筛选新增「客户名称」下拉、「客户ID」由文本框升级为下拉（候选取 `GET /api/customer`，均保留手动输入任意值），并各含「其他（无客户信息）」项——选中发送哨兵 `__none__`，后端 `applyTraceTagFilters` 据此匹配**无任何客户信息**的日志（客户名按写时快照字符串 LIKE 匹配）。客户名称 / 客户ID / 下游请求ID / 下游TraceTag 四项统一移到高级筛选区**最后一行**；主筛选行时间范围收窄、类型加宽；「错误关键字」移到高级区**单独末行**（避免其随类型选择性出现时挤动其他框）。从「明细错误」下钻到使用日志时，筛选栏新增只读「错误签名」项回显当前过滤条件（随 `errorSignature` 透传、可被重置清除）。
- **计费详情金额行调整**：「折扣后金额」更名为「渠道折后金额」(带 `(x分组统计倍率)`)；「客户折扣后金额」展示 `(x客户折扣)`（后端新增写 `other.customer_discount`，按客户×模型解析）；在其后新增「利润金额」行＝客户折扣后金额 − 渠道折后金额（二者都存在时显示，可为负）。
- **Prometheus `newapi_error_total` 新增 `attribution` 标签**：与 `short_name` 并列记录错误归因（空值回退 `uncategorized`，低基数、不受 `ERROR_METRIC_HIGH_CARD_LABELS` 开关影响）；落点取 `model.ResolveErrorAttribution(signatureHash)`。注意该指标 series schema 多一标签，历史与新 series 会并存。

### 修复 Fixed
- **「编辑归一化规则」弹窗在明文 HTTP/内网 IP 下白屏 500（Error Signature Rules Dialog Crash）**：弹窗生成规则行的客户端 id 用了 `crypto.randomUUID()`，而该 API 仅在**安全上下文**（HTTPS 或 localhost）下有定义；经 `http://<内网IP>` 访问后台时 `crypto.randomUUID` 为 `undefined`，弹窗一打开即抛 `TypeError`、被前端 error boundary 接管渲染成「500」页（后端 `/api/log/error-signature-rules` 实际返回 200，并非服务端 500）。改用项目已在用的 `nanoid()`（底层 `crypto.getRandomValues`，明文 HTTP 下亦可用）。全局仅此一处使用 `crypto.randomUUID`。需重新 `bun run build` 前端生效。

## [2026-10-22]

### 新增 Added
- **客户管理与客户折扣（Customer Management & Discount）**：以「下游 Trace-Tag 日志」为基础的独立新功能。新增管理员「客户」页面（与「用户」平级，`/customers`，admin 守卫）维护「客户ID（唯一索引）→ 客户名」映射 + 备注 + `(客户×模型)` 折扣（含客户级默认折扣），后端 `/api/customer`（AdminAuth）+ `model.Customer` 表（`model_discounts` 以 JSON 存 TEXT 列，零第二表）+ 内存缓存（变更重建，热路径只读内存）。消耗日志侧：详情展示「客户名」（trace-tag 解出 `customer_id` 后就近查缓存、**写时快照**，无映射则空）；计费详情新增「客户折扣后金额」=**原价（分组倍率=1）× 客户折扣**（复用渠道统计金额的 `finalizeTextQuotaForGroupRatio`，走统一 quota 取整/clamp），写入 `other.customer_stat_quota`，仅对解出客户ID的请求落库。折扣为乘数（`0.8`=8折）、**仅统计不改实际扣费**（扣费口径逐分不变，text_quota 固定值回归用例全绿）。`customer_id` 经 `common.TraceTagFromContext` 一次解码缓存到 gin.Context，结算与日志两处共用。详见 `docs/customer-management-and-discount.md`。
- **下游网关 Trace-Tag 日志记录与检索（Downstream Trace-Tag Logging）**：链路为「客户 → 下游网关 → 本层 new-api → 上游网关」，下游网关在请求头 `x-bce-aigw-trace-tag` 带一个确定性认证加密（SIV：`HMAC-SHA256(macKey, 明文)[:16]` 作 IV + `AES-256-CTR`，base32 无填充）的值，本层解密还原出「下游 request_id」与「客户 ID（下游 `api_key_id`，可空＝公开模式）」，连同原始 tag 一并写入**消耗日志(type=2)与错误日志(type=5)** 的 `other` JSON（`downstream_trace_tag`/`customer_id`/`downstream_request_id`，零迁移、不加列），在日志详情**管理员可见**，并支持按这三者检索（`GetAllLogs`/`GetUserLogs` 新增 query 参数，`other` 做跨方言 `LIKE`，元字符转义）。子密钥由 `AIGW_TRACE_TAG_KEY`（64 位 hex 主密钥）经 HKDF-SHA256 派生（`sync.Once` 缓存）；密钥未配置/非法时打一次 `SysError` 并禁用解码、**不影响启动**；认证失败只保留原始 tag、绝不静默解成错误 ID；解码在热路径仅微秒级、且全程不影响日志写入。注意：`other` 无索引为全表扫描，检索需配合时间范围收窄。详见 `docs/downstream-trace-tag-logging.md`。
- **日志按错误关键字筛选（Log Filter by Error Keyword）**：通用日志筛选栏「高级筛选」区新增「错误关键字」输入框，对日志 `content` 列做包含（LIKE）匹配，**仅在日志类型选为「错误」时才展示、才生效**。错误文本由 `RecordErrorLog` 写入 `content`，故按关键字排障等价于 `content LIKE %关键字%`；用户输入的 LIKE 元字符会被转义后再包裹 `%...%`。前后端双保险——前端仅在类型=错误时渲染并在切换类型时清空，后端 `GetAllLogs` 仅当 `logType == LogTypeError` 且关键字非空时才追加条件，避免误触发全表扫描。跨方言 LIKE 由 `model/log.go` 的 `errorContentLikeCondition` 统一构造（SQLite/MySQL/PostgreSQL 用 `ESCAPE '!'`，ClickHouse 用反斜杠转义）。按范围仅管理端 `GetAllLogs` 支持；零迁移、不加列。注意：只命中错误日志、`content` 无索引为全表扫描（已被类型+时间范围收窄）、不影响顶部用量/RPM/TPM 统计。详见 `docs/log-filter-error-keyword.md`。
- **明细错误统计（Detailed Error Stats）**：把「归一化后的错误串」称为**签名**，并允许管理员按**简名（明细错误类型）**归类（多签名→一简名，未配置归「未分类」），便于把隐藏在 400 等状态码里的各类错误固定下来统计/检索/对外暴露。写错误日志时（`processChannelError`）实时归一化：`Content` 改存**未脱敏原文**（`err.ErrorWithStatusCode()`，响应仍脱敏），并把签名与其 `sha256` 指纹写入日志既有的 `other` JSON（`error_signature`/`error_signature_hash`）——**复用现有可扩展字段、不新增列、不改表结构**，对 SQLite/MySQL/PostgreSQL/ClickHouse 全部日志库零 schema 变更。归一化是一组**有序纯正则规则** `{name,pattern,replacement,enabled}`（RE2，支持 `$1`），默认删 `(request id:…)` 后缀、掩 UUID/长 hex/按 key 的 request id/ISO 时间戳；规则集可在页面编辑并持久化为 Option 行 `ErrorSignatureRules`，启动时应用、空则用默认，**顺序有意义、仅对之后日志生效**。「使用日志」同级新增管理员页「明细错误统计」：按签名聚合展示次数/占比/首末时间/状态码/模型·渠道 chips、内联编辑简名、「推荐规则」（基于实际签名给候选正则）、下钻复用使用日志列表（`GetAllLogs` 新增 `error_signature_hash` 精确过滤）。Prometheus 新增 `newapi_error_total{short_name,model,channel,status,protocol,stream,customer}`，按当时简名累加、`customer` 暂用 username，高基数标签（model/customer）由 `ERROR_METRIC_HIGH_CARD_LABELS`（默认开）开关。接口均 `AdminAuth`：`/api/log/error-stats`、`/api/error-alias`(增删查)、`/api/log/error-signature-rules`(读写+`/suggestions`)。注意：错误日志（type=5）默认非仅管理员可见，存原文依赖「本部署不开放普通用户」前提，若启用普通用户需将其加入 `adminOnlyLogTypes` 或改为原文只入 `admin_info`；聚合在缺省时间窗时会扫描并在内存聚合（上限 5 万行后截断），建议配合时间范围。详见 `docs/detailed-error-stats.md`。

## [2026-10-21]

### 新增 Added
- **渠道统计分组倍率（Statistics Group Ratio）**：为“分组倍率配成 0 以免扣费/耗额度”的免费透传渠道（如 `behind-cdnaigw`）解决统计金额归零的问题。在不改变实际扣费的前提下，结算时额外计算两个“仅统计”金额——**原始金额**（分组倍率=1 的原价）与**折扣后金额**（按渠道级 `stat_group_ratio`），写入每条消费日志 `other`（`raw_quota`/`stat_quota`/`stat_group_ratio`），并在日志详情弹窗展示。统计倍率存于渠道 `setting` JSON（`dto.ChannelSettings.StatGroupRatio`，零迁移；缺省/非正数按 1 即折扣=原价），在「渠道额外设置」组配置、渠道列表以 `×x` 展示。计费公式重构为“分组倍率=1 基数 × 任意倍率”三次求值，扣费口径与改前逐字节一致（原有固定值回归用例全绿）。注意：`tiered_expr` 计费渠道的这两个统计字段按标准公式计算、可能与表达式扣费不一致（不影响实际扣费）。详见 `docs/channel-stat-group-ratio.md`。
- **未配置价格模型放行开关（Allow Unset Model Ratio）**：在「系统设置 → 运维 → System Behavior」新增全局开关 `AllowUnsetModelRatio`（默认关）。开启后，价格与倍率均未配置的模型请求不再返回 `400`，而是按兜底倍率 `37.5 × 分组倍率` 计费放行（复用既有 accept-unset 分支，与「自用模式」「用户级 `AcceptUnsetRatioModel`」为「或」关系）。零迁移、不加列，走 `options` K-V 持久化；已配价/已配倍率模型不受影响。详见 `docs/allow-unset-model-ratio.md`。
- **日志按状态码筛选（Log Filter by Status Code）**：通用日志筛选栏「高级筛选」区新增「状态码」输入框，按 HTTP 状态码过滤。状态码目前仅错误日志落库（`controller/relay.go` 写 `other.status_code`），故匹配日志 `other` JSON（`"status_code":N`，以 `,`/`}` 定界避免 `400` 误命中 `4000`），零迁移、不加列；`GetAllLogs`/`GetUserLogs` 均支持。注意：本质是筛错误码（填 `200` 查不到成功请求），`other` 无索引为全表扫描，且不影响顶部用量/RPM/TPM 统计。详见 `docs/log-filter-status-code.md`。
- **自动调度（Auto Scheduling，阶段一）**：在「系统设置 → 模型与路由 → 自动调度」下新增配置页，维护 VM（VictoriaMetrics）数据源、PromQL 规则模板库（带 `__占位符__`，含取值标签 `result_label` 与 `rank`/`exclude` 策略位 `result_mode`）、以及规则（对象分组/用户/Key × 模型 → 模板 + 占位符参数），并可用真实数据源预览 PromQL 结果。网关只做占位符替换 + 转发 VM `/api/v1/query` 瞬时查询，不自实现 PromQL。数据模型为三张 `x_` 前缀表（`x_schedule_datasource`/`x_schedule_rule_template`/`x_schedule_rule`）；数据源密码不随接口下发。**本阶段仅配置与预览，不接入真实请求路由**。详见 `docs/auto-scheduling.md`。
- **按用户月度开销汇总（CSL Hourly Monthly）**：新增管理员接口 `GET /api/log/monthly`，按月汇总调用方分组内各用户的结算总额（人民币 `settlement_price_cny` 与美元 `settlement_price_usd`），按金额降序返回。`month=YYYY-MM` 指定月份、省略为当月（当月统计截止到当前时刻，未来月份拒绝）。内部复用小时账单查询，将整月按 168 小时窗口分段聚合（每小时仅属一个窗口，不重复计数），沿用 `CslHourlyAuth` 鉴权并要求管理员角色。

### 优化 Changed
- **小时账单查询时间跨度上限放宽**：`GET /api/log/hourly` 与 `GET /api/log/self/hourly` 的查询时间跨度上限由 7 天放宽到 60 天（控制器 `maxCslHourlySpanSeconds` 与 OmniDataSearch 读取路径的 `cslHourlyMaxHours` 同步调整，仍对 SQL `IN` 列表与总行数保留防护）。

## [2026-10-20]

### 新增 Added
- **日志筛选模型/分组自动候选（Log Filter Autocomplete）**：通用日志筛选栏的「模型名称」「分组」从纯文本框升级为可搜索候选下拉（combobox），候选取全站（模型 `GET /api/channel/models_enabled`，即已配置且启用的模型；分组 `GET /api/group/`，均管理员专用，非管理员退化为纯手输框），并保留手动输入任意值。提交参数、URL 同步不变；分组字段因改用候选下拉不再做敏感信息掩码。详见 `docs/log-filter-autocomplete.md`。
- **渠道密钥掩码显示（Channel Key Masking）**：渠道密钥保存后在编辑抽屉展示掩码（前 10 + 中间星号 + 后 6；短密钥整体打码；多密钥逐行），便于核对是否填错，无需 2FA。掩码在后端读取时即时计算、不新增数据库列，完整密钥绝不随详情接口下发；查看完整密钥仍走原有安全验证（Passkey/2FA）接口且仅超级管理员可用。详见 `docs/channel-key-masking.md`。
- **渠道代理池（Proxy Pool）**：在「系统设置-模型」维护全局代理表（别名 → 地址），渠道按别名多选引用并按运行时优先级顺序故障转移；被动健康采样（连接层失败累计触发冷却与半开重试，不起主动探测），别名为空或全不健康时回退渠道原「代理地址」文本框。代理信息仅记入 `admin_info`。代理池条目支持备注列。详见 `docs/proxy-pool.md`。
- 前端维护页展示当前版本并支持查看本更新日志（构建期打包 `CHANGELOG.md`）。

### 优化 Changed
- 日志筛选栏体验微调：候选下拉项与输入框在内容过长被截断时，鼠标悬停显示完整文本（原生 `title`）；主筛选行按比例调宽窄——时间范围、所有类型收窄，分组加宽。

## [2026-10-19]

### 新增 Added
- **客户批量模型映射**：渠道级按「基础模型 × 客户」自动生成别名模型 `<model>_<customer>`，保存时展开写入 `Models`/`ModelMapping`，用于在共享渠道（如 behind-cdnaigw）日志里按 `model_name` 区分下游客户；别名对外隐藏、计费回退到基础模型。详见 `docs/customer-batch-model-mapping.md`。

## [2026-10-18]

### 新增 Added
- 管理员创建用户时可选同时创建一个默认 Key。

## [2026-10-17]

### 新增 Added
- 禁用渠道等危险操作增加二次确认。

## [2026-10-13 ~ 2026-10-16]

### 新增 Added
- **会话 ID 日志记录与检索**（分三期）：按 relay 格式提取会话 ID（Claude 取 `metadata.user_id` 内嵌 `session_id`；GPT 取 `prompt_cache_key`/`client_metadata.session_id`）写入日志 `other`，在日志详情展示并标注来源；新增通用异步分片扫描引擎 `pkg/logscan`，支持在通用日志按会话 ID 检索。详见 `docs/session-id-logging-and-search.md`。
- 日志支持记录 `node_name`，便于集群多节点排障。
- 日志支持指定上游请求 ID 响应头来源。
- **Claude metadata 注入开关 + 缓存 TTL 选项**：为缺 `metadata.user_id` 的 Claude `/v1/messages` 请求合成仿 Claude Code 的 `user_id` blob（内嵌派生 `session_id`），提升上游会话黏滞与缓存命中率；可配置强制 cache TTL（含 1h）。详见 `docs/claude-metadata-session-injection.md`、`docs/claude-cache-hit-rate-optimization.md`。
- **响应过滤规则（Response Filter）**：渠道级按路径/键名从上游成功响应体清洗指定字段（起因 Azure 内容过滤器细节泄露给客户），接入 OpenAI/Azure 的 chat 与 Responses；解析失败安全回退原 body。详见 `docs/response-filter-design.md`。

## [2026-10-15]

### 新增 Added
- 复制 behind-cdnaigw 渠道时自动建分组（0 倍率 + 可用分组）与无限额度 Key。
- **覆盖规则命名模板**：把 param_override / retry_override 规则抽成命名模板集中维护，渠道按名多选引用，改模板后所有引用渠道运行时立即生效。详见 `docs/override-rule-templates.md`。

## [2026-10-09 ~ 2026-10-14]

### 新增 Added
- **Debug 模式运行时开关**：Debug 接入 option 系统，管理员可即开即关无需重启；DEBUG 级日志单独写入 `debug-*.log`，不再刷 stdout/stderr。详见 `docs/debug-mode-runtime-toggle.md`。
- **Session Cookie 多实例隔离**：会话 cookie 名可配置，解决同 host 不同端口部署时登录态互相覆盖。详见 `docs/session-cookie-isolation.md`。

### 优化 Changed
- 重试覆盖规则：新增「200 响应体内含错误」时的改写/重试动作；参数覆盖日志从调试日志中分离。

## [2026-09-19 ~ 2026-09-24]

### 新增 Added
- **重试覆盖（Retry Override）四段式规则**：上游报错或 200 响应体含错误时，按「请求侧过滤 → 错误匹配 → 请求体改写 → 原渠道/下一渠道重试」规则生效；流式改为「提交前缓冲」检测；param/retry 覆盖过程落库为控制台可见日志。详见 `docs/retry-override-design.md`。
- Prometheus 指标导出。

### 修复 Fixed
- 适配 MySQL 5.6：解决索引长度限制，修复覆盖规则日志前端查询。
- 客户端提前断开后保持上游流继续读完并正常计费（计费行为分析见 `docs/claude-stream-disconnect-billing.md`）。

## [2026-08]

### 新增 Added
- 新增 logshipper：日志双写走 logshipper，适配 ClickHouse 集群字段输出。
- csl-hourly 接口支持可读时间范围参数；`relayBasePath` 下仅白名单 path 可访问。

### 优化 Changed
- 合并上游 main 多项修复（用户大额度溢出、硬删除清理鉴权数据、未定价模型页、MiniMax 识别、GORM v2 行锁等）。

