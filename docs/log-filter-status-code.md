# 日志按状态码筛选（Log Filter by Status Code）

## 背景

通用日志（使用日志）里，错误请求的详情列会展示 `status_code=400, ...` 这类上游/网关返回的 HTTP 状态码。运维排障时常需要「只看某个状态码的请求」（如 429 限流、400 参数错误、5xx 上游故障），但筛选栏此前没有对应条件，只能逐页肉眼找。

## 目标

在通用日志筛选栏「高级筛选」区新增「状态码」输入框，按 HTTP 状态码过滤日志。改动尽量轻：不加数据库列、不做迁移。

## 关键决策

- **匹配日志 `other` JSON，而非新增列**：状态码目前只在错误日志落库——`controller/relay.go` 在记录 `LogTypeError` 时写 `other["status_code"] = err.StatusCode`。因此按状态码筛选等价于在 `other`（TEXT 列）里匹配 `"status_code":N`。此方案零迁移、改动最小。
- **值用 `,`/`}` 定界，避免前缀误命中**：`other` 是紧凑 JSON（`common.MapToJsonStr`，冒号后无空格），状态码是整数，命中模式为 `%"status_code":400,%` 或 `%"status_code":400}%`（跟随逗号或右花括号），防止 `400` 误命中 `4000`。数字不含 LIKE 元字符，无需转义。
- **跨方言 LIKE**：条件由 `model/log.go` 的 `statusCodeOtherCondition` 统一构造，`LIKE ? OR LIKE ?`，对 SQLite/MySQL/PostgreSQL/ClickHouse 均成立。
- **管理端与「仅自己」都支持**：`GetAllLogs` 与 `GetUserLogs` 均加 `statusCode` 参数，与既有 `request_id` 一致。

## 已知限制（注意）

- **本质是筛错误码**：成功请求（200）不往 `other` 写 `status_code`，所以该筛选只能命中错误日志；填 `200` 查不到成功请求。如需覆盖成功码，需改为「新增 `status_code` 列 + 迁移 + 各写日志路径回填」的方案。
- **无索引、全表扫描**：`other` 是无索引 TEXT，`LIKE` 命中靠扫描，适合小时间范围的错误排障，不适合大跨度高频查询。
- **不影响顶部统计**：`用量/RPM/TPM` 统计接口（`SumUsedQuota`）未接该参数，统计数字不随状态码筛选变化。

## 实现

- 后端
  - `model/log.go`：新增 `statusCodeOtherCondition(column, statusCode)`；`GetAllLogs` / `GetUserLogs` 末尾新增 `statusCode int` 参数，非 0 时追加该 WHERE 条件。
  - `controller/log.go`：`GetAllLogs` / `GetUserLogs` 两个 handler 解析 `status_code` 查询参数（`strconv.Atoi`）并透传。
- 前端（`web/default/src/features/usage-logs/`）
  - `types.ts`：`CommonLogFilters.statusCode?: string`、`GetLogsParams.status_code?: number`。
  - `routes/.../usage-logs/$section.tsx`：搜索 schema 新增 `statusCode`。
  - `lib/filter.ts`：`buildSearchParams` 带上 `statusCode`。
  - `lib/utils.ts`：仅正整数时映射为后端 `status_code`。
  - `components/common-logs-filter-bar.tsx`：高级筛选区「上游请求 ID」后新增「状态码」输入框（纯数字、回车搜索），接入 draft/sourceKey/重置/计数。
- i18n：文案键 `Status Code` 在 en/zh/zh-TW/fr/ja/ru/vi 均为既有键，无需新增。

## 验证

- 后端 `go vet ./model/ ./controller/` 通过；`gofmt` 无差异。
- 前端 `bun run typecheck` 通过；改动文件 `oxlint` 0 error（仓库存量 lint 问题与本次无关）。
