# 更新日志 / Changelog

本文件记录 new-api 网关（本 fork，分支 `feature/custom-query-api`）在上游之上的自定义变更。
格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。详细设计见 `docs/` 下对应文档。

> 说明：本文件在前端构建时被打包进「系统设置 → 运维 → 维护」页。修改后需重新执行 `bun run build` 才会在页面生效。

## [2026-10-21]

### 新增 Added
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

