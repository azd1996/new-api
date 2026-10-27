# 定制模块清单（重新基线化搬运 Checklist）

> 本文档盘点当前分支相对上游主干的全部定制改动，作为"从最新主干重开分支 → 按模块搬运"的搬运清单。

## 基线信息

- 定制分支：`feature/custom-query-api`
- 上游参照：`upstream/main`（QuantumNous/new-api）
- 分叉点（merge-base）：`a63364d1`，2026-07-14
- 定制 commit 数：**101**（不含 merge）
- 落后上游：**497** commit；上游改动 3056 文件（+328k/-210k），含前端目录重构（`web/default/` → `web/`）
- 生成日期：2026-10-27

## 图例

- 🟢 **新增为主**：多为新增文件，搬运成本低，几乎可直接拷贝
- 🟡 **半新增半侵入**：新增文件 + 少量上游文件插桩
- 🔴 **深度侵入**：改动上游热点文件，需改写成 hook 式隔离，冲突风险高

## 搬运优先级速览

- 先搬（易、独立）：B、C、N、H(代理池/客户映射)、K(新增部分)
- 后搬（难、压在上游热点）：E(Claude)、F(Retry Override)、J(log.go)、L(billing/金额)

---

## A. 基础设施 / 环境隔离 — 🟡（5）

小范围环境/隔离改造，散落插桩于 main 与 common。

- 关键文件：`main.go`、`common/`、`model/log.go`、`controller/user.go`
- commits：
  - `343a6bc8b` add redis key prefix
  - `bf0357647` feat(cookie): cookie isolation
  - `716e31c44` feat(debug) add debug runtime switch
  - `ba84d516e` feat(log): add node_name into logs
  - `c28503305` feat(user): optionally create a default key when admin creates a user

## B. CSL-Hourly 数据接口 + RELAY_BASE_PATH — 🟢（7）

独立数据接口 + relay 路由白名单，基本自包含。

- 关键文件：`controller/csl_hourly.go`、`model/csl_hourly.go`、路由注册、`RELAY_BASE_PATH` 相关
- commits：
  - `3c2ed3a43` feat(csl) add csl_hourly api
  - `d1ff3d5aa` feat(csl): add query_start&query_end; add RELAY_BASE_PATH
  - `f67530063` feat(csl): fix RELAY_BASE_PATH router
  - `40e391f16` change CslHourly struct
  - `096723fb6` feat(csl-hourly): 可读时间范围参数 + relayBasePath 白名单
  - `dc203b7c8` feat(csl-hourly): 使用本地 module 名称，规避敏感信息
  - `8c263d050` feat(csl-hourly): add OmniDataSearch reader as alternative data source

## C. Logshipper / ClickHouse 双写 — 🟢（2）

日志外发到 ClickHouse 集群，配合"log other→真列双写"策略。

- 关键文件：logshipper 新增模块、日志双写字段输出
- commits：
  - `d5513f417` feat: add logshipper
  - `52ff5634a` feat: 调整双写走 logshipper 的字段输出，适应 clickhouse 集群

## D. Relay 稳定性 — 🔴（1）

- 关键文件：relay 流式主流程
- 注意：上游已有同主题 fix，搬运时需与上游实现对齐（勿简单覆盖）
- commits：
  - `12bcbcc7e` fix(relay): keep upstream stream alive after client disconnect

## E. Claude 链路定制 — 🔴（6）

Claude Code 客户端相关：metadata 注入、cache TTL、affinity、session 缓存。上游该区域改动密集，重点冲突区。

- 关键文件：`relay/claude_handler.go`、`dto/claude.go`、`relay/channel/claude/relay-claude.go`
- commits：
  - `bfb763462` feat(claude): parse message_start string
  - `cc2330e19` feat(affinity) add claude meta_data if not exist
  - `482ceb577` feat(claude): remove meta_data.session_id
  - `b83e80ddc` feat(claude): support force cache ttl 1h
  - `c5a2192c7` feat(claude): add metadata injection switch & modify cache ttl option
  - `04553bfc4` feat(claude): better metadata/cachettl option

## F. Retry Override / Param Override（含命名规则集模板）— 🔴（22）

最大模块。四阶段重试覆盖规则 + 共享求值器、200 中途限流检测、全格式接入、命名规则集模板、规则日志落库。

- 关键文件：`relay/common/override.go`、各 relay handler、`model/channel.go`、前端 Override Rules UI、`.my-rules/*.json`
- 注意：任务1 的"200 强制 fallback"已移除，勿改回
- commits：
  - `ca7feabe6` feat(relay): thinking-family 400 fallback retry on original channel
  - `b6d1c4f33` refactor(retry): rework retry-override into four-phase rules with shared evaluator
  - `027dab6a6` refactor(retry): generalize thinking fallback into response-aware retry override
  - `52bbcb35d` refactor(retry): simplify thinking fallback to a single param-override transform
  - `e8d747036` feat(retry): wire retry-override for all formats + 200-body detection (non-stream)
  - `32d7bb0ca` feat(retry): streaming 200-body detection with mid-stream fallback (Part C) + UI
  - `247507691` feat(retry): drop duplicate preamble on retry-override stream fallback
  - `9b820566a` feat(retry): add per-operation action (same channel vs fallback)
  - `c4f6aa172` fix(retry): read retry_override from the full channel, not the loop stub
  - `e680be3d8` fix(param-override): skip invalid operations instead of failing the request
  - `2e0cfb97c` feat(retry): 200 中途限流检测调整为 hold 前面若干 chunk
  - `f1a237dbf` feat(retry): 参数覆盖和重试覆盖增加日志，控制台可查
  - `ec408d863` feat(retry): 适配 mysql5.6，修复覆盖规则日志前端查询
  - `62216ad81` feat(retry): 解决 mysql5.6 的索引长度限制问题
  - `dc5b147f8` feat(retry): modify retry rule when status200
  - `c560807f0` feat(retry): override template（命名规则集模板）
  - `8d4c151d1` feat(retry): seperate debug log & remove param-override log
  - `23723b49c` feat(override): fold param-override audit lines by array index
  - `ad5d222ef` chore(retry): add INFO logging for retry-override debugging
  - `1aabfb369` refactor(web): move retry-rules UI into the Override Rules section
  - `b16d842c2` feat(retry): upload my retry-override rules and param-override rules
  - `ebd966f14` feat(retry): upload 200-error-body-flow.md（文档）
  - `a2ce806e4` chore: update local param rules and add playground storage doc

## H. 渠道能力增强 — 🟡（5）

代理池、客户批量模型映射偏新增；密钥掩码、渠道复制偏侵入。

- 关键文件：`controller/channel.go`、`model/channel.go`、`dto/channel_settings.go`、代理池新增模块、前端渠道编辑抽屉
- commits：
  - `bcbd5c900` feat(channel): auto-provision group & key when copying behind-cdnaigw channel
  - `5a9e5e046` feat(channel): add customer batch model mapping
  - `617145948` feat(channel): add proxy pool with passive health and failover
  - `de6c03d66` feat(channel): add description column to proxy pool entries
  - `082c9c57e` feat(channel): show masked key in edit drawer for verification

## I. 客户（Customer）维度 — 🟡（7）

客户管理页、前缀系列折扣、客户折后金额、下游客户归因、客户统计看板。

- 关键文件：`model/`(customer/quota_data)、`service/`、`controller/`、前端 customers feature
- commits：
  - `575463229` feat(customers): add customers admin page UI (table, drawer, route, sidebar)
  - `2cc980ea8` feat(customers): prefix-based series discounts + log customer filter dropdowns
  - `4fb76975a` feat(metrics): prefer downstream customer id for error metric customer label
  - `989ef4369` feat(dashboard): add customer amount metrics
  - `4abfc63d3` feat(dashboard): add admin Customer Statistics section
  - `ee327581f` feat(error-stats): add per-signature customer breakdown column
  - `4b3e5a140` fix(customer): ignore legacy default discount

## J. 消耗 / 错误日志增强 — 🔴（22）

第二大模块。session_id（真列+搜索）、logscan 异步扫描引擎、trace-tag 解析、多维过滤。`model/log.go` 为重灾区。

- 关键文件：`model/log.go`、`controller/log.go`、`pkg/logscan/logscan.go`、前端 usage-logs feature
- commits：
  - `2788bf4c7` feat(logs): promote session_id to a real logs column
  - `8963ae435` feat(log): record and display session_id (phase 1)
  - `93aa7fb2b` feat(logscan): generic async chunked scan engine (phase 2)
  - `db73bdd02` feat(log): session-id log search backend + API client (phase 3)
  - `fa903f3b0` feat(log): session-id search UI in common logs filter bar (phase 3)
  - `f88b24377` feat(log): annotate session id source in log detail
  - `f824b73d7` feat(log): shorten synthesized session-id source label
  - `a329c9a89` feat(log): modify fallback string
  - `965082537` feat(logs): decode downstream trace-tag header into logs with search
  - `38b2ea51f` feat(log): support specify upstream request id header
  - `785a90797` feat(logs): add error-keyword filter to common logs
  - `2a33fda14` feat(logs): add status-code filter to common logs
  - `4bcf94c34` feat(logs): add monthly per-user spend summary, widen hourly span to 60d
  - `09bced676` feat(logs): autocomplete model/group filters with site-wide candidates
  - `a72ccf69e` fix(logs): use enabled channel models for filter candidates
  - `340b6ba5e` feat(logs): tweak filter bar widths and show full text on hover
  - `6abf93a69` style(logs): widen date range, narrow type in filter row
  - `c908c13db` feat: add log filter include exclude support
  - `14ce13693` fix(logs): support multi-value filters
  - `06ed9b4ed` fix(logs): contain multi-select chips
  - `66b31c849` feat(logs): net generation speed with precise latency fallback
  - `3f9565d41` feat(logs): add usage statistics frontend prototype

## K. 明细错误统计（Error Stats）— 🟡（8）

签名归一化 + 简名分类、规则集可编辑 + 跨实例同步、下钻、Prometheus 指标。新增文件为主 + 日志写路径侵入。

- 关键文件：`model/error_stats.go`、`pkg/logscan/`、前端 error-stats feature、`model/log.go`(error_signature_hash)
- commits：
  - `c4a757201` feat(logs): add detailed error stats (signature + short-name categories)
  - `d8b0bf4a3` refactor(logs): store error signature in Other JSON instead of new columns
  - `e6a387d91` feat(logs/metrics): stat amounts + profit, error attribution, filter/UI tweaks
  - `161d4c296` feat(error-stats): per-signature note column, vendor-id default rule, layout tweaks
  - `ee46627d5` fix(error-stats): use nanoid for rule-row ids to avoid crypto.randomUUID crash
  - `274c41606` feat(error-stats): cross-instance rule sync, column drill-down, full dropdown candidates
  - `0cfa73da3` feat(logs): dual-write error_signature_hash into a real logs column
  - `ba37747b9` feat(logs): add customer and category filters to error stats

## L. 数据看板 / 金额口径 / 计费 — 🔴（6）

三口径金额（原始/渠道折后/客户折后）、分流、渠道统计倍率、允许无价模型。正面压在上游 billing 重构上。

- 关键文件：`service/text_quota.go`、`relay/helper/price.go`、`types/price_data.go`、quota_data、前端 dashboard feature
- commits：
  - `bd028db5e` feat(dashboard/logs): stat-amount calibers across dashboard + log column dual-write
  - `bb060e5e8` feat(dashboard): expand flow tooltip metrics
  - `bb824d839` fix(dashboard): surface quota_data write failures instead of swallowing them
  - `50d06f7d9` feat(billing): add channel statistics group ratio for raw/discounted amounts
  - `aacae6d89` feat(billing): add global switch to allow models without configured price
  - `62ff1f022` feat(logs): track discount fallback and retry overrides

## M. 性能监控 / 过载保护 / 系统设置 — 🟡（9）

监控与过载保护分离、进程级指标、过载/中断落日志、Prometheus 导出、System Behavior 配置、维护页版本。

- 关键文件：`middleware/performance.go`、`middleware/stats.go`、`common/system_monitor.go`、`common/performance_config.go`、`controller/perf_metrics.go`、前端 system-info/system-settings feature
- commits：
  - `1d68af68f` feat(performance): separate monitoring and overload protection
  - `0c6732192` feat(logs): record system overload rejections
  - `d5009da15` feat(logs): record process shutdown interruptions
  - `766a782ac` feat(system-info): add process-level metrics to instance panel
  - `53d65ae78` feat(prometheus): 增加 prometheus 指标导出
  - `12b3ea827` feat(system-settings): add System Behavior option frontend and cross-instance cache sync
  - `55830f6be` feat(web): show version and changelog on maintenance page
  - `5b87e192b` fix(docker): bundle repo-root CHANGELOG.md into default frontend build
  - `bd2f30822` feat(safety): double-confirm disable

## N. 自动调度（Scheduling）— 🟢（1）

VM(PromQL) 指标驱动的自动调度配置 + 预览，phase 1 未接真实调度。

- 关键文件：`controller/x_schedule.go`、3 张 `x_` 前缀表、前端 scheduling feature
- commits：
  - `abc3f109c` feat(scheduling): add VM-metric auto-scheduling config & preview (phase 1)

---

## 搬运执行建议

1. 从当前 `upstream/main` 拉新分支作基底（自带前端新结构 + 497 commit 修复，含 CVE/OOM/Claude/billing 修正）。
2. 按 B → C → N → H → K → I → M → A → D → E → F → J → L 顺序搬（先独立后侵入）。
3. 侵入模块（🔴）一律改写为"独立文件 + 上游文件插一行 hook"，每搬完一个模块 `gofmt && go test`。
4. 前端（E/F/H/I/J/K/L/M/N 的 UI 部分）需在上游新的 `web/src/` 结构重落，无法平移 `web/default/`。
5. L（金额口径）与上游 `cc/cc1h/cc12h` 表达式引擎冲突最大，建议最后搬并单独评估。




