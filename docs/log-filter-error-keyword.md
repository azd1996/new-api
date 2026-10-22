# 日志按错误关键字筛选（Log Filter by Error Keyword）

## 背景

通用日志（使用日志）里，错误请求的错误信息（上游/网关返回的报错文本）落在日志 `content` 列。运维排障时常需要「只看包含某段报错文本的错误日志」（如 `rate limit`、`content_filter`、`timeout`、某个上游错误码文案），但筛选栏此前没有对应条件，只能逐条点开详情肉眼找。

## 目标

在通用日志筛选栏「高级筛选」区新增「错误关键字」输入框，对日志 `content` 做子串（LIKE）匹配。**仅在日志类型选为「错误」时才生效、才展示**。改动尽量轻：不加数据库列、不做迁移。

## 关键决策

- **匹配 `content` 列的包含子串**：错误文本由 `RecordErrorLog` 写入 `content`。筛选等价于 `content LIKE %关键字%`。用户输入的关键字按「包含匹配」处理——后端对其 LIKE 元字符（`%`、`_`、转义符）做转义后再包裹 `%...%`，用户自己输入的 `%` 不会被当通配符。
- **仅错误日志生效（前后端双保险）**：
  - 前端：错误关键字输入框只在「日志类型 = 错误」时渲染；切换到其它类型时清空该字段，不随查询下发。
  - 后端：`GetAllLogs` 仅在 `logType == LogTypeError` 且关键字非空时才追加该 WHERE 条件；即使前端误传，其它类型也不会触发 `content` 全表 LIKE。
- **跨方言 LIKE**：条件由 `model/log.go` 的 `errorContentLikeCondition` 统一构造，对 SQLite/MySQL/PostgreSQL 用 `LIKE ? ESCAPE '!'`、对 ClickHouse 用 `LIKE ?`（反斜杠转义），与既有 `sessionOtherLikeCondition` 同构。
- **仅管理端**：按确认范围，仅 `GetAllLogs`（管理员通用日志）支持，`GetUserLogs`（仅自己）不加该参数。

## 已知限制（注意）

- **只命中错误日志**：成功请求不写错误文本，且条件被 `type=error` 门控，填任何关键字都查不到成功请求。
- **无索引、全表扫描**：`content` 是无索引 TEXT，前导通配符的 LIKE 靠扫描。但查询已被 `type=error` + 时间范围（命中 `idx_created_at_type` 复合索引）大幅收窄，适合小时间范围的错误排障，不适合大跨度高频查询。
- **不影响顶部统计**：`用量/RPM/TPM` 统计接口（`SumUsedQuota`）未接该参数，统计数字不随错误关键字筛选变化。

## 实现

- 后端
  - `model/log.go`：新增 `errorContentLikeCondition(column, keyword)`；`GetAllLogs` 末尾新增 `errorKeyword string` 参数，仅当 `logType == LogTypeError` 且非空时追加 `content LIKE` 条件。
  - `controller/log.go`：`GetAllLogs` handler 解析 `error_keyword` 查询参数并透传。
- 前端（`web/default/src/features/usage-logs/`）
  - `types.ts`：`CommonLogFilters.errorKeyword?: string`、`GetLogsParams.error_keyword?: string`。
  - `routes/.../usage-logs/$section.tsx`：搜索 schema 新增 `errorKeyword`。
  - `lib/filter.ts`：`buildSearchParams` 带上 `errorKeyword`。
  - `lib/utils.ts`：有值时映射为后端 `error_keyword`。
  - `components/common-logs-filter-bar.tsx`：高级筛选区「状态码」后新增「错误关键字」输入框，仅在类型=错误时渲染；切换类型时清空；接入 draft/sourceKey/重置/计数。
- i18n：新增文案键 `Error Keyword`，补齐 en/zh/zh-TW/fr/ja/ru/vi。

## 验证

- 后端 `go build ./...` 通过；`gofmt` 无差异。
- 前端 `bun run typecheck` 通过；`bun run i18n:sync` 无缺失键。
