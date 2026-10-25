# 客户管理与客户折扣（Customer Management & Discount）

> 本需求以「下游网关 Trace-Tag 日志记录」（见 `docs/downstream-trace-tag-logging.md`）为基础：trace-tag 解密出的 `customer_id`（下游 `api_key_id`）是本功能的索引键。但这是一个**独立的新功能**。

## 1. 要解决的问题

共享的 behind-cdnaigw 渠道汇聚了多个下游客户的流量。我们已能从 `x-bce-aigw-trace-tag` 解出每条请求的 `customer_id`，但：

- `customer_id` 是机器编码，不可读 —— 需要一个「客户ID → 客户名」的映射，便于在日志里认人。
- 不同客户、不同模型有不同的商务折扣 —— 需要按 `(客户 × 模型)` 维度配置折扣，并在日志里算出「客户折扣后金额」用于对账。

## 2. 方案总览

新增管理员「客户」页面（与「用户」页面平级）维护客户配置；消耗日志在结算时按客户折扣算出一个**仅统计**金额落库展示。

**折扣是仅统计/展示属性，绝不改变实际扣费**（与已有的渠道 `raw_quota`/`stat_quota` 同类）。这是刻意设计：本层对 behind-cdnaigw 走 0 倍率免费透传，折扣只服务于对账。

## 3. 数据模型

`model.Customer`（表 `customers`，AutoMigrate 注册）：

| 字段 | 说明 |
|-|-|
| `customer_id` | 客户ID，`varchar(64)` **唯一索引**，必填 |
| `name` | 客户名（展示用） |
| `remark` | 备注（自由文本） |
| `default_discount` | 历史兼容字段，保留数据库列但不再展示或参与计算 |
| `prefix_discounts` | `(模型名前缀 → 折扣倍率)` 映射（即「系列折扣」，如 `claude` / `gpt` / `gemini` / `grok`），序列化为 JSON 存 `TEXT` 列（API 字段 `prefix_discounts`；Go 字段 `PrefixDiscounts`/`PrefixDiscountMap`）|

折扣值为**乘数**：`0.8` 表示打 8 折 / 付 80%。规范化：任何 `≤0`/`NaN`/`Inf` 的折扣一律按 `1` 处理（`normalizeDiscount`）；保存时丢弃前缀为空或非法的折扣项。

折扣查询优先级（`model.CustomerModelDiscount(customerID, model)`）：
1. 在 `prefix_discounts` 中取**命中该模型名的最长前缀**（大小写不敏感 `HasPrefix`）的折扣
2. 无任何前缀命中 → `1`（按原始金额统计，忽略历史默认折扣）
3. 客户ID 无映射 → `1`（不打折）

> **按前缀（系列）而非精确模型**：早期设计曾用精确的 `(模型 → 折扣)` 映射（旧列 `model_discounts`），后改为按模型名前缀配置一组「系列折扣」并去掉精确模型折扣——因为同一系列（claude/gpt/…）通常共用一档折扣，前缀更省配置、也便于前端按字符串排序展示。
>
> **旧列 `model_discounts` 的处理（方案1：保留悬空列）**：改名没有走「重命名列」或「`gorm:"column:"` 复用旧列」，而是新增 `prefix_discounts` 列。原因：① GORM `AutoMigrate` 无法表达列重命名，只会加新列；② 手工重命名跨库语法不统一（MySQL 5.7.8 不支持 `RENAME COLUMN`）且需处理幂等；③ 旧列存的是「完整模型名→折扣」，语义与新的「前缀→折扣」不同，直接沿用数据是错的。鉴于本功能为全新上线、几乎无正式数据，代码层已彻底不再引用 `model_discounts`，DB 中若已建出该列则**原样保留为悬空列**（不读不写、无迁移、无 DROP），跨 SQLite/MySQL/PostgreSQL 均零风险。

## 4. 缓存

客户配置小且改动少，全量常驻内存（`model/customer_cache.go`）：`sync.Once` 首次构建、每次增删改后 `refreshCustomerCache()` 重建。日志热路径（查名字 + 查折扣）只读内存，不查库。

**多实例同步**：缓存是每实例独立的内存副本，而增删改只刷新“处理该写请求的那一个实例”。为保证在另一实例上的改动能传播到所有节点（否则其它节点要等进程重启才更新），启动时挂了一个后台轮询协程 `go model.SyncCustomerCache(common.SyncFrequency)`（`main.go`，紧挨 `SyncOptions`/`SyncChannelCache`），每隔 `SYNC_FREQUENCY` 秒（默认 60s）`refreshCustomerCache()` 从 DB 全量重载。因此新增/改名最长约一个同步周期即全集群生效，**无需重启**；代价是存在最长 ≤ `SYNC_FREQUENCY` 的传播延迟（DB 轮询，非实时推送）。

## 5. 结算与落库（仅统计）

复用「渠道统计金额」的数学（`service/text_quota.go`）：`calculateTextQuotaSummary` 已把金额抽成分组倍率=1 的基数 `combinedUnit` + `finalizeTextQuotaForGroupRatio(ratio)`。

- `CustomerStatQuota = finalizeTextQuotaForGroupRatio(combinedUnit, modelRatio, customerDiscount)` —— 即**原价（分组倍率=1）× 客户折扣**（公式对 ratio 线性，等价于 `raw_quota × discount`，走统一的 quota 取整/clamp）。
- 客户ID 通过 `common.TraceTagFromContext(ctx)` 取得（该 helper 一次解码、缓存到 `gin.Context`，与日志写入侧共用，避免重复解码）。
- 仅当请求**确实解出了 customer_id** 时，才把 `other["customer_stat_quota"]` 写入消耗日志；未识别客户的请求不写该字段。
- `扣费口径逐分不变`：只新增一次 reporting 计算，`summary.Quota`（实际扣费）不受影响；现有 text_quota 固定值回归用例全绿。

## 6. 客户名落库（写时快照）

`customer_id` 在 `model.RecordConsumeLog → attachDownstreamTraceTag` 解出后，就近查客户缓存，若有映射则把 `other["customer_name"]` 写入日志（**写时快照**）。含义：

- 日志生成时已配置映射 → 记录当时的客户名；
- 未配置映射 → 不写（详情展示为空）；
- 后续改名或新增映射 **不回溯** 旧日志（与「折扣后金额必须写时计算」一致）。

> 结合第 4 节：新增客户后，尚未同步到该映射的实例（最长 `SYNC_FREQUENCY` 秒内）写出的日志会只有 `customer_id`、无 `customer_name`；这类“空名”属写时快照、不会回溯补名，等各节点同步后新日志才带名。若发现持续无名，先确认客户管理里的 `customer_id` 与日志解出的值（下游 `api_key_id`）**完全一致**。

## 7. 前端

- 管理员「客户」页面：`web/default/src/features/customers/` + 路由 `routes/_authenticated/customers/index.tsx`（admin 守卫，`role < ADMIN` 跳 `/403`）+ 侧边栏 `use-sidebar-data.ts` 的 admin 组新增「Customers」项。CRUD 列表（customer_id / name / 系列折扣 / remark，系列折扣列按前缀字符串排序、以「N 个系列」+ 悬浮明细展示）、新建/编辑抽屉（含「系列折扣」动态行编辑：前缀 + 折扣，longest-prefix 生效 + 未命中时按 1 统计 + 备注）。后端 `/api/customer`（AdminAuth）。
- 日志详情 `details-dialog.tsx`：概览区在「客户ID」上方增加「客户名」行（管理员可见、有值才显示）；计费详情在「折扣后金额」之后增加「客户折扣后金额」行（`other.customer_stat_quota`）。
- `LogOtherData` 增 `customer_name` / `customer_stat_quota`。
- 通用日志筛选（管理员，`common-logs-filter-bar.tsx`）：新增「客户名称」下拉与「客户ID」下拉（候选取 `GET /api/customer`，均 `allowCustomValue` 可手输），并提供「其他（无客户信息）」项——选中后发送哨兵 `__none__`，后端 `applyTraceTagFilters` 据此匹配**无任何客户信息**的日志；客户名称/客户ID/下游请求ID/下游TraceTag 四项统一放到高级筛选区**最后一行**。查询参数经 route schema（`customerName`/`customerId`/…）→ `buildApiParams` 映射为后端 `customer_name`/`customer_id`。

## 8. 相关文件

- 后端：`model/customer.go`、`model/customer_cache.go`、`model/main.go`（迁移）、`controller/customer.go`、`router/api-router.go`、`service/text_quota.go`、`common/trace_tag.go`（`TraceTagFromContext`）、`model/log.go`（`attachDownstreamTraceTag` 写 `customer_name`；`applyTraceTagFilters` 支持 `customer_name` LIKE 与 `__none__` 无客户哨兵）、`controller/log.go`（解析 `customer_name` query）、`main.go`（`SyncCustomerCache` 周期重载）。
- 前端：`web/default/src/features/customers/*`、`routes/_authenticated/customers/index.tsx`、`hooks/use-sidebar-data.ts`、`features/usage-logs/components/dialogs/details-dialog.tsx`、`features/usage-logs/components/common-logs-filter-bar.tsx`、`features/usage-logs/lib/{filter,utils}.ts`、`features/usage-logs/types.ts`、`routes/_authenticated/usage-logs/$section.tsx`。

## 9. 边界与已知取舍

- 折扣仅统计、不扣费；`tiered_expr` 计费渠道的 `customer_stat_quota` 按标准公式（非 tiered）计算，可能与表达式扣费不一致（不影响实际扣费），与 `raw_quota`/`stat_quota` 口径一致。
- `customer_id`、`customer_name`、`customer_stat_quota` 落在 `other` 顶层，`GetUserLogs` 会随 payload 返回给非管理员；前端详情按 `isAdmin` 隐藏。本部署内 user≈下游网关账号、为其自身请求数据，风险低；若需服务端强隔离可改嵌 `other.admin_info`。
