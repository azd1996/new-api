# 日志查询效率可优化点（ClickHouse 日志库）

本文档记录在 ClickHouse 作为日志库（`LOG_SQL_DSN` 指向 ClickHouse）场景下，对现有日志查询路径的分析结论。只记录待办与分析结果，不代表已实施；实施后请在对应条目后补充完成说明或直接删除该条目。

涉及的主要文件：

- [model/log.go](../model/log.go) — 日志主表 `LogFilter`/`GetAllLogs`/`GetUserLogs`/`StreamAllLogs` 等
- [model/log_analysis.go](../model/log_analysis.go) — 日志分析弹窗（延迟/FRT/缓存命中/成功率）
- [model/log_session.go](../model/log_session.go) — 会话视图（`log_sessions` 物化视图）
- [model/error_stats.go](../model/error_stats.go) — 明细错误统计
- [model/cache_skeleton.go](../model/cache_skeleton.go) / [model/cache_skeleton_analysis.go](../model/cache_skeleton_analysis.go) — 缓存骨架与会话缓存分析
- [model/csl_hourly.go](../model/csl_hourly.go) / [model/csl_hourly_omni.go](../model/csl_hourly_omni.go) — 小时账单汇总
- [model/main.go](../model/main.go) — ClickHouse 建表/迁移 DDL

## 1. `status_code` 落地为真实列（状态：已实施）

- **问题**：`statusCodeOtherCondition`（[model/log.go:124-132](../model/log.go:124)）对未索引的 `logs.other` 大文本列做 `LIKE '%"status_code":NNN,%' OR LIKE '%"status_code":NNN}%'`，每次按状态码筛选（日志列表、`error-stats`）都是逐行字符串扫描。
- **方案**：给 `Log` 结构体和 ClickHouse 建表 DDL 新增真实的 `StatusCode int` 列，在 `RecordErrorLog`/`middleware/performance.go` 等错误日志写入路径把 `other.adminInfo["status_code"]` 同步写入新列，`LogFilter.apply`/`ErrorStatFilter` 改为对新列做等值匹配。
- **影响面**：`model/log.go`（结构体、迁移 DDL、`apply`）、`model/error_stats.go`（`applyErrorStatFilter`）、`model/log_session.go`（如需透传）、写入路径（`service/relay_error.go`、`middleware/performance.go`、`service/violation_fee.go`、`pkg/logshipper`）。
- 历史行只在 `other` JSON 里有 `status_code`，新列读历史行应為 0；筛选时为兼容旧数据，可保留"新列优先、旧数据回退 Other LIKE"的双路径，或者接受历史数据在新列上查不到（因为错误日志默认有 TTL，影响有限）。

## 2. 高基数精确匹配字段补充 ClickHouse 跳数索引（bloom_filter）（状态：已实施）

- **问题**：`logs` 表的 `ORDER BY (created_at, request_id)` 之外，原来只有 `session_id` 有 `bloom_filter` 跳数索引。`request_id`、`upstream_request_id`、`downstream_request_id`、`customer_id`、`error_signature_hash` 这些高频精确匹配字段都没有对应跳数索引，宽时间范围下的精确查询需要逐 part 扫描。
- **方案**：新增 `clickHouseLogBloomFilterColumns`（[model/main.go](../model/main.go)）统一列出需要加索引的字段（`session_id`、`request_id`、`upstream_request_id`、`downstream_request_id`、`customer_id`、`error_signature_hash`），`clickHouseLogCreateTableSQL` 通过 `clickHouseLogSkipIndexesSQL()` 为新装机的建表 DDL 拼出全部 `INDEX ... TYPE bloom_filter(0.01) GRANULARITY 4` 子句；`migrateClickHouseLogDB` 对已有表遍历同一个列表执行 `ALTER TABLE logs ADD INDEX IF NOT EXISTS idx_logs_xxx xxx TYPE bloom_filter(0.01) GRANULARITY 4`，两处共用同一份列表，不会再出现建表 DDL 和迁移脚本各加各的、逐渐漂移的问题。
- **影响面**：`model/main.go`（`migrateClickHouseLogDB`、`clickHouseLogCreateTableSQL`、新增 `clickHouseLogBloomFilterColumns`/`clickHouseLogSkipIndexesSQL`）。纯 DDL 改动，不涉及查询代码。
- **注意**：跳数索引只覆盖新写入的 part，旧 part 需要等待后台 merge 或人工 `MATERIALIZE INDEX`（`migrateClickHouseLogDB` 里已有相关注释）。

## 3. `GetLogAnalysis` 的延迟/FRT 分桶与百分位计算下推到 ClickHouse SQL

- **问题**：`GetLogAnalysis`（[model/log_analysis.go:288-341](../model/log_analysis.go:288)）用 `Rows()` 流式扫描 consume 日志，在 Go 侧对每行解析 `other` JSON 取出 `lat`/`frt`/`cache_tokens` 等字段，再手写排序求 p50/p90/p99（`nearestRankPercentile`，[model/log_analysis.go:204-217](../model/log_analysis.go:204)）。有 `logAnalysisScanLimit=500000` 行上限，超限即标记 `Truncated`，意味着大范围/高流量查询下统计结果是不完整采样而非真实聚合。
- **方案**：ClickHouse 原生支持 `quantile`/`quantilesExact`/`JSONExtractFloat` 等函数（`logSessionRowSelect` 已经示范了 `JSONExtractFloat(logs.other, 'cache_tokens')` 的写法，见 [model/log_session.go:67-72](../model/log_session.go:67)），理论上可以把分桶（按 `prompt_tokens` 分档）、延迟均值/百分位、缓存命中分类全部用一条或几条 `GROUP BY channel_id, bucket` 的 SQL 聚合完成，省去把几十万行拉到应用层的网络与内存开销，同时消除截断问题。
- **影响面**：`model/log_analysis.go` 核心聚合逻辑，改动较大，需要仔细保证统计口径（分桶边界、Claude cache 输入口径等）与现有实现完全一致。建议单独立项评估收益与改造成本后再排期。

## 4. `fillLogSessionIntervals` 的请求间隔计算下推到 ClickHouse SQL

- **问题**：[model/log_session.go:409-463](../model/log_session.go:409) 为计算会话请求间隔（min/max/avg gap），对命中 session 的全部 consume 日志时间戳做 `Rows()` 流式扫描，在 Go 里按 session 分组排序求差值，上限 `logSessionScanLimit=200000` 行。
- **方案**：ClickHouse 有 `groupArray`/`arraySort`/`arrayDifference`/`quantile` 等数组函数，理论上可以用 `SELECT session_id, groupArray(created_at) ...` 配合 `arraySort`+`arrayDifference` 在 SQL 侧一次性算出每个 session 的间隔分布，彻底省掉把时间戳全量拉到应用层的开销。
- **影响面**：`model/log_session.go` 的 `fillLogSessionIntervals`，逻辑相对复杂（需要保留"跳过早于 `first_at` 的历史行"等边界条件），建议作为单独任务评估，收益随会话规模增长而增大。

## 5. `GetAllLogs`/`StreamAllLogs` 的 channel 名称解析未走批量缓存接口

- **问题**：[model/log.go:975-1013](../model/log.go:975) 与 [model/log.go:1056-1067](../model/log.go:1056) 在 `MemoryCacheEnabled` 为真时，对每页日志涉及的每个 `channelId` 单独调用 `CacheGetChannel`，没有走批量接口。量级通常不大（一页内不同 channel 数有限），收益有限，属于低优先级的小优化。
- **方案**：如果存在批量读取 channel 缓存的接口，可以替换为一次批量调用；否则不建议为此单独新增接口。

## 6. `GetLogSessions` 的 count + 分页两次查询可考虑合并或并发

- **问题**：[model/log_session.go:319-337](../model/log_session.go:319) 先执行一次 `SELECT count() FROM (query)`，再执行一次分页查询，两次都要对 `log_sessions`（`AggregatingMergeTree`，查询时才真正 `GROUP BY session_id` 合并）做一次完整的重新聚合。
- **方案**：
  - 如果允许近似总数，可以用 ClickHouse 的近似计数函数替代精确 `count()`，减少一次聚合；
  - 如果需要精确总数，暂时无法避免两次扫描，但可以把 count 查询和分页查询并发执行，节省一次 RTT（当前是顺序执行）。
- **影响面**：`model/log_session.go` 的 `GetLogSessions`，优先级较低，建议结合实际压测数据决定是否投入。

## 优先级排序

1. `status_code` 落地为真实列（改动小、影响面广）— **已实施**
2. 高基数字段补充 bloom_filter 跳数索引（纯 DDL，无需改查询代码）— **已实施**
3. `GetLogAnalysis` 延迟/百分位计算下推 SQL（收益大，改造成本较高，需单独评估）
4. `fillLogSessionIntervals` 间隔计算下推 SQL（收益随会话规模增长，需单独评估）
5. channel 名称批量查询、`GetLogSessions` count 并发化（低优先级，视压测结果再定）
