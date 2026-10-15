# 会话 ID 日志记录与查询 方案文档

## 1. 背景与目标

为排查 Claude / GPT 流量的会话连续性与上游缓存命中情况，需要在日志里记录每条请求的「会话 ID」，并支持在「使用日志」页面按会话 ID 检索该会话的全部请求。

目标拆成三期，逐期独立可交付：

- **一期**：把会话 ID 写入日志，并在「日志详情」里展示一行「会话 ID」（形态类比现有「上游请求 ID」）。
- **二期**：实现一个通用、与业务无关的「异步分片扫描引擎」`pkg/logscan`。
- **三期**：基于该引擎实现 session 查询（管理员「通用日志」按会话 ID 检索）。

## 2. 会话 ID 的来源定义

统一成一行「会话 ID」，后端按 relay 格式取值，优先级固定：

- **Claude 原生**：最终 `metadata.user_id` blob 里的 `session_id`（我们注入的 16hex 或客户端自带的值）。
- **GPT Responses**：`prompt_cache_key` 优先，为空回退 `client_metadata.session_id`。
- **GPT Chat Completions**：`prompt_cache_key`。
- **抽取不到**：写入字面量 `"notfound"`（用于「无会话 ID」检索，见三期）。
- **其它格式 / 非 relay 日志**（Gemini、充值、管理、登录等）：不写该 key。

## 3. 一期：会话 ID 写入日志 + 详情展示

### 3.1 存储决策：放进 `other` JSON，不单独立列

会话 ID 存进现有日志的 `other` JSON 字段（如 `other.session_id`），**不新增数据库列**。理由：

- 对所有现有链路无感：主库（SQLite/MySQL/PG）无需 AutoMigrate 加列/建索引；in-process ClickHouse（`LOG_SQL_DSN=clickhouse`）的 `CREATE TABLE` 已含 `other`，无需改 DDL / ALTER 存量表；文件 shipper（`pkg/logshipper`）的字段白名单已含 `other`，会话 ID 自动随 `other` 搭车，**外部 ClickHouse 集群表也无需加列**。
- 代价：按会话 ID 检索只能走 `other LIKE` 全表扫描（无索引），这正是二/三期用「分片 + 缓存 + 异步」来兜住的点。
- 放弃项：独立索引列能支持的 request_id ↔ session_id 索引联动，本期不做（涉及 DB 链路，成本高，后续再议）。

### 3.2 捕获点（三处 relay 路径）

在构造上游请求时抽取会话 ID，经 context key 传递，最终在 `service/log_info_generate.go` 的 `GenerateTextOtherInfo` 写入 `other`：

- Claude：`relay/claude_handler.go`，metadata 处理块之后，解析最终 `metadata.user_id` blob 的 `session_id`。
- GPT Responses：`relay/responses_handler.go`，取 `prompt_cache_key`，空则 `client_metadata.session_id`。
- GPT Chat：`relay/compatible_handler.go`，取 `textReq.PromptCacheKey`。

抽到即写真实值；抽不到写 `"notfound"`；仅上述会话相关格式的消费/错误日志写该 key。

### 3.3 详情页展示

前端 `web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx` 概览区（「节点名称」行之后）新增一行「会话 ID」，读取 `other.session_id`（空值不渲染，行为对齐现有 `request_id`/`upstream_request_id`）。`types.ts` 的 `LogOtherData` 增加 `session_id?: string`。i18n 新增 `Session ID` → 「会话 ID」。

### 3.4 影响面与验证

- 无数据库迁移、无外部集群改动；老日志无该 key，不显示该行（预期）。
- 验证：后端 `gofmt` + 相关 `go test`；前端 `bun run typecheck` / `lint` / `i18n:sync`。

## 4. 二期：通用异步分片扫描引擎 `pkg/logscan`

### 4.1 定位

把「异步 + 分片 + 文件缓存 + 进度 + 强制刷新」的编排逻辑做成一个**与业务无关**的引擎，放在 `pkg/logscan`。它不认识 session、`other LIKE`、gin，只负责编排；「查什么」由调用方通过回调注入。

### 4.2 API 草图

```
type JobSpec struct {
    CacheKey  string        // 调用方给的规范化 key，引擎 hash 成文件名
    KeyHint   string        // 可选人类可读前缀（引擎 sanitize）
    Start, End int64
    Chunk     time.Duration // 1h
    Force     bool          // 命中缓存也备份重跑
    ScanChunk func(ctx context.Context, win Window) ([][]byte, error) // 每片怎么查
}

type Status struct { State string; Total, Done, Matched int; Err string }

func Submit(spec JobSpec) (handle string, err error)        // 起后台任务 / 命中缓存直返
func Query(cacheKey string) (Status, bool)                  // 轮询状态/进度
func Read(cacheKey string, page, size int) ([][]byte, int)  // 分页读结果（JSON 行）
```

引擎处理每行一条已 marshal 的 JSON（`[][]byte`），**不依赖 `model.Log`**。

### 4.3 引擎职责（业务无关）

- 缓存查找：命中完成文件直接返回；存在 `.ing` 返回 running + 进度；都没有则起后台任务。
- Force：已有完成文件先备份（`.bak.<ts>`）再重跑。
- 同 key single-flight 锁；全局并发上限（默认 2，超出排队）。
- 按 `Chunk` 把 `[Start,End]` 切成窗口，逐片调用 `ScanChunk`，结果以 JSON 行追加写 `<file>.ing`。
- 全部完成后 `.ing` → 最终文件；旁写 `<file>.meta`（total/done/matched/status/err）供轮询。
- 文件名 = `<KeyHint安全化>_<hash12>`，hash 来自 `CacheKey`：既防路径穿越/非法字符/超长，又把完整缓存键编码进文件名。
- TTL 清理（默认 24h，可配）；文件落在 `*common.LogDir/session_search/`（需配置 `LOG_DIR`）。

### 4.4 关键注意

- **后台独立 context**：任务 goroutine 用引擎内部派生的 `context.Background()`，不能用 gin 请求 context（handler 返回即被 cancel）。
- 引擎内无任何业务词（session/notfound/prompt_cache_key）——这些只活在调用方。

### 4.5 可测性

用假的 `ScanChunk`（返回确定性行、不碰 DB）即可测编排/缓存/并发/TTL/Force；无需数据库。

## 5. 三期：session 查询（引擎的适配器）

### 5.1 适配器 service

`service/session_log_search.go` 只懂 session 语义，把请求翻译成一个 `JobSpec`：

- 计算 `CacheKey = canonical(sessionID | username | start | end)`（缓存键 = sessionid + 用户名 + 时间范围，换范围各自缓存、结果正确）。
- `ScanChunk`：对每个 1h 窗口执行
  `LOG_DB.Where("username = ?", u).Where("created_at >= ? AND created_at < ?", w0, w1).Where("other LIKE ?", pattern)`，
  命中行 marshal 成 JSON 行返回。
- `pattern = %"session_id":"<转义值>"%`，复用现有 `buildLogLikeCondition` 的转义 / ESCAPE 逻辑；「无会话 ID」时值为 `notfound`。

### 5.2 校验规则（前后端双重，后端为准）

- 必填：用户名 + 时间范围。
- 时间范围 ≤ 10 天；若 sessionID == `notfound` 则进一步 ≤ 1 天（notfound 命中多，防缓存文件过大）。
- 不按日志 `type` 过滤（匹配该用户该时间段内任意命中会话的日志）。

### 5.3 控制器 / 路由（仅管理员）

`controller/log_session.go` 对接 HTTP ↔ 适配器，纯转译。新增管理员接口：启动/查询 + 轮询结果（分页读结果文件）。注册在管理员日志路由组。

### 5.4 前端

`web/default/src/features/usage-logs/components/common-logs-filter-bar.tsx`：

- 新增 `sessionId` 文本输入；「搜索」旁新增「强制搜索」按钮。
- `sessionId` 非空：前端校验用户名 + 时间范围（≤10 天；值为 `notfound` 时 ≤1 天并提示），走异步接口 + 轮询进度，结果用现有表格渲染。
- 「强制搜索」：带 `force=true`，命中缓存也备份重跑；`sessionId` 为空时等同普通「搜索」。
- `sessionId` 为空：两个按钮都走普通 `GetAllLogs`。
- `api.ts` 加启动/轮询函数，react-query `refetchInterval` 轮询；i18n 新增 `Force Search` 等文案。

## 6. 跨期约束与默认取舍

- 存储位置默认 `other` 顶层 `session_id`。
- 缓存/备份文件 TTL 默认 24h、并发上限默认 2、目录 `LOG_DIR/session_search/`（需配置 `LOG_DIR`）。
- 文件名对客户端可控的 `prompt_cache_key` 做 sanitize + hash，禁止原样拼接。

## 7. 风险与回滚

- 一期零迁移、零外部依赖，风险最低，可独立上线。
- 二期为纯新增包，不触碰现有查询路径，可 feature-flag。
- 三期检索走 `other LIKE` 全扫，靠分片 + 用户名 + 时间范围收窄 + 并发上限控制 DB 负载；HTTP 层不阻塞其它用户，DB 负载影响同步/异步一致，已用并发上限兜底。
- 回滚：各期独立，关闭入口/删除新增包即可，无数据结构变更。

## 8. 验证清单

- 后端：`gofmt`、相关 `go test`（含 `pkg/logscan` 引擎单测、适配器 pattern/校验单测）。
- 前端：`bun run typecheck`、`lint`、`i18n:sync`。
