# 渠道统计分组倍率（Channel Statistics Group Ratio）

## 背景

部分渠道（典型如 `behind-cdnaigw` 标签下、按部门 1:1 映射上游的渠道）把**分组倍率配成 0**，目的是让这些请求不扣费、避免触发额度耗尽。但副作用是：系统里一条请求只有一个 `quota` 字段，它既是“要扣的钱”又是“统计口径”。分组倍率为 0 时，`quota = (prompt+completion) × modelRatio × groupRatio` 直接被打成 0，于是所有按渠道汇总的统计（`SumUsedQuota`、`quota_data` 看板、`channel.used_quota`）全部为 0——看不到各渠道消耗的**原始金额**与**折扣后金额**。

本功能在**完全不改变实际扣费**的前提下，额外计算并记录两个“仅统计”金额，让消耗重新可见。

## 设计

### 两个仅统计金额

结算时在“扣费额度”之外，用同一套公式额外求两个值：

- **原始金额 `raw_quota`**：按分组倍率 = `1` 计算（未打折的“原价”）。
- **折扣后金额 `stat_quota`**：按**渠道级统计倍率** `stat_group_ratio` 计算。

实际扣费 `quota` 仍按真实分组倍率（可能为 0）计算，用户 / 令牌额度照常不受影响。

### 渠道级统计倍率

- 存储：`dto.ChannelSettings.StatGroupRatio *float64`，落在渠道 `setting` JSON 列里（复用既有列，**零迁移、不加列**）。
- 语义：`nil` / `≤0` / `NaN` / `Inf` 一律按 `1` 处理，即“折扣后金额 = 原始金额”。
- 配置入口：渠道编辑抽屉「渠道额外设置」组内的「统计分组倍率」输入框（`step=any`，可填任意正小数，如 `0.875`）；渠道列表在名称下以 `Stat Ratio: ×x` 小字展示已配置的倍率。

### 计费等价性（关键不变量）

`service/text_quota.go` 的金额计算重构为：先算出「分组倍率 = 1、且未乘 other-ratios 的基数 `combinedUnit`」，再由 `finalizeTextQuotaForGroupRatio` 对任意分组倍率求最终额度，一次请求调用三次——扣费（真实倍率）、原始（1）、折扣（统计倍率）。

由于整条额度对分组倍率**线性**（surcharge / audio 也按倍率缩放，`ApplyOtherRatios` 是乘常数、齐次），且 `shopspring/decimal` 的 `Mul`/`Add` 为精确运算，`combinedUnit × g` 与原内联公式逐字节相等。因此**扣费口径 `quota` 与重构前完全一致**，原有固定值回归用例（如 1488 / 4884 / 180000）全部保持通过；新增用例 `TestCalculateTextQuotaSummaryReportingQuotas` 覆盖分组倍率 0、价格分支、缺省回落等场景。

### 落库与展示

- 每条消费日志的 `other` JSON 追加 `raw_quota` / `stat_quota` / `stat_group_ratio` 三个字段。
- 日志详情弹窗在「总费用」上方展示「原始金额」「折扣后金额」（复用既有金额格式化；仅在字段存在时显示；统计倍率非 1 时折扣行带 `(x倍率)` 后缀）。

## 已知限制

- **tiered 表达式计费**：当渠道启用 `tiered_expr` 计费时，实际扣费 `quota` 由表达式结果覆盖，而 `raw_quota` / `stat_quota` 仍按标准 token×倍率公式计算，二者可能对不上。这**不影响实际扣费**，仅使该渠道的这两个统计字段在 tiered 场景下不精确。`behind-cdnaigw` 等不使用 tiered，故不受影响。

## 后续（阶段二，未实现）

把 `raw_quota` / `stat_quota` 落入 `quota_data` 滚动表（加两列 + 三库迁移 + 聚合查询），在数据看板按渠道、按时间范围汇总两金额。

## 相关位点

- `dto/channel_settings.go`：`ChannelSettings.StatGroupRatio`
- `service/text_quota.go`：`calculateTextQuotaSummary` / `finalizeTextQuotaForGroupRatio` / `PostTextConsumeQuota`
- `service/text_quota_test.go`：等价性与新字段回归用例
- 前端：`features/channels/lib/channel-form.ts`、`features/channels/components/drawers/channel-mutate-drawer.tsx`、`features/channels/components/channels-columns.tsx`、`features/usage-logs/components/dialogs/details-dialog.tsx`
