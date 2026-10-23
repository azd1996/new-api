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
| `default_discount` | 客户级默认折扣倍率（覆盖未单独配置的模型；缺省 1） |
| `model_discounts` | `(模型 → 折扣倍率)` 映射，序列化为 JSON 存 `TEXT` 列（API 字段 `model_discounts`，DB 列同名字符串）|

折扣值为**乘数**：`0.8` 表示打 8 折 / 付 80%。规范化：任何 `≤0`/`NaN`/`Inf` 的折扣一律按 `1` 处理（`normalizeDiscount`）；保存时丢弃非法的模型折扣项。

折扣查询优先级（`model.CustomerModelDiscount(customerID, model)`）：
1. `model_discounts[model]`（该客户该模型的专属折扣）
2. 否则 `default_discount`（客户级默认）
3. 客户ID 无映射 → `1`（不打折）

## 4. 缓存

客户配置小且改动少，全量常驻内存（`model/customer_cache.go`）：`sync.Once` 首次构建、每次增删改后 `refreshCustomerCache()` 重建。日志热路径（查名字 + 查折扣）只读内存，不查库。

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

## 7. 前端

- 管理员「客户」页面：`web/default/src/features/customers/` + 路由 `routes/_authenticated/customers/index.tsx`（admin 守卫，`role < ADMIN` 跳 `/403`）+ 侧边栏 `use-sidebar-data.ts` 的 admin 组新增「Customers」项。CRUD 列表（customer_id / name / default_discount / 模型折扣 / remark）、新建/编辑抽屉（含「模型折扣」动态行编辑 + 默认折扣 + 备注）。后端 `/api/customer`（AdminAuth）。
- 日志详情 `details-dialog.tsx`：概览区在「客户ID」上方增加「客户名」行（管理员可见、有值才显示）；计费详情在「折扣后金额」之后增加「客户折扣后金额」行（`other.customer_stat_quota`）。
- `LogOtherData` 增 `customer_name` / `customer_stat_quota`。

## 8. 相关文件

- 后端：`model/customer.go`、`model/customer_cache.go`、`model/main.go`（迁移）、`controller/customer.go`、`router/api-router.go`、`service/text_quota.go`、`common/trace_tag.go`（`TraceTagFromContext`）、`model/log.go`（`attachDownstreamTraceTag` 写 `customer_name`）。
- 前端：`web/default/src/features/customers/*`、`routes/_authenticated/customers/index.tsx`、`hooks/use-sidebar-data.ts`、`features/usage-logs/components/dialogs/details-dialog.tsx`、`features/usage-logs/types.ts`。

## 9. 边界与已知取舍

- 折扣仅统计、不扣费；`tiered_expr` 计费渠道的 `customer_stat_quota` 按标准公式（非 tiered）计算，可能与表达式扣费不一致（不影响实际扣费），与 `raw_quota`/`stat_quota` 口径一致。
- `customer_id`、`customer_name`、`customer_stat_quota` 落在 `other` 顶层，`GetUserLogs` 会随 payload 返回给非管理员；前端详情按 `isAdmin` 隐藏。本部署内 user≈下游网关账号、为其自身请求数据，风险低；若需服务端强隔离可改嵌 `other.admin_info`。
