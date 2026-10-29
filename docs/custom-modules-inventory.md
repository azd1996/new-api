# 定制模块清单（重新基线化搬运 Checklist）

> 本文档盘点当前分支相对上游主干的全部定制改动，作为"从最新主干重开分支 → 按模块搬运"的搬运清单。

## 基线信息

- 定制分支：`feature/custom-query-api`
- 上游参照：`upstream/main`（QuantumNous/new-api）
- 分叉点（merge-base）：`a63364d1`，2026-07-14
- 定制 commit 数：**101**（不含 merge）
- 落后上游：**497** commit；上游改动 3056 文件（+328k/-210k），含前端目录重构（`web/default/` → `web/`）
- 生成日期：2026-10-27

## 当前总进度（2026-10-29 全量收口）

- rebase 工作区：`/Users/aizhendong/Desktop/Projects/work/ai/ai-gateway/new-api-rebase`，分支 `rebase/v1.0.0-rc.47-20261025`（工作树干净，全部已提交；共 67 个定制 commit）。
- **后端 A–N 可搬部分已全部落地**；**前端 web/src 重落也已全部完成**（N/I/K/H+F/L/J+L/M+D+E+F/system-info/A 各有提交）；**feature 分支快照后的增量 eda601c(debug-scope)/558a4f1(前端 perf) 也已补搬**。`bunx tsc -b` 全绿 + `bun run build` 成功 + 改动文件 oxlint 0 error。
- **代码类定制已全部搬完**。剩余的只有「有意不搬/因后端未搬而延后」的项（下方"仍未搬清单"）、未经真实库验证的 DB 迁移、以及待补的非英文译文。
- ⚠️ **未经真实库验证的 DB 迁移**（待 owner 在含 ClickHouse 环境跑）：L 的 logs 5 金额列 + quota_data 2 列；J/K 的 session_id/trace-tag/customer_id/downstream_request_id/error_signature_hash 列；M 的 retry_triggered 列。SQLite 已验，MySQL/PG/ClickHouse 待验。
- ⚠️ **前端运行时未实测**（本地未起服务，仅验编译/构建/lint）；**非英文译文统一待补**（英文源串天然可用；仅 N 调度已带简中 `efbe8bccb`）。
- ⚠️ **rebase 既有红测（非本次搬运引入）**：`TestOaiResponsesStreamHandlerCompletionLifecycle`（relay/channel/openai）在干净 HEAD 即 FAIL，待单独排查。

## 仍未搬清单（有意跳过 / 依赖未搬后端而延后）

- **H 客户批量模型映射**（`5a9e5e046`）：按 owner 要求跳过（涉及 price.go 计费）。
- **L 客户统计看板**（`989ef4369` customer amount metrics、`4abfc63d3` Customer Statistics）：后端未搬 → 前端 customer-charts 一并延后（依赖 quota_data.customer_id 聚合 + `/api/data/customers`）。
- **J 日志剩余**：include/exclude、多值过滤、月度消耗汇总、net 生成速度（后端）、usage-statistics 原型 → 后端读单标量/无端点，前端一并延后。
- **M 维护页 changelog/版本**（`55830f6be`）+ Docker 打包 CHANGELOG（`5b87e192b`）：延后（无端点/孤儿）。
- **M System Behavior 跨实例即时 pub/sub**（`12b3ea827` 的 pub/sub 增量）：配置区已搬，pub/sub 实时同步按上游 wsmanager 范式重做为可选延后项。
- **routing-reliability-section（前端）**：**非 fork 定制，无需搬**。该文件在 merge-base `a63364d1` 即已存在（上游原有，最后改动者 CaIon 2026-07-11，fork 从未提交过改动）；上游后来把它重构成 `request-policies` feature（系统设置→请求策略：Request checks / Sessions and retries / Channel health 三子页）。属上游演进，rebase 用上游新版即可。
- **558a4f1 后端合并 stat 扫描**：rebase 该 stat 路径无 fork 要合并的第二个扫描，无可合并、强搬会动语义，跳过（前端 perf 已搬）。
- **docs/\*.md 与 `.my-rules/*.json`**（含 `05e49d3`）：按既定政策不搬（聚焦代码、避免 docs 膨胀）。

## 图例

- 🟢 **新增为主**：多为新增文件，搬运成本低，几乎可直接拷贝
- 🟡 **半新增半侵入**：新增文件 + 少量上游文件插桩
- 🔴 **深度侵入**：改动上游热点文件，需改写成 hook 式隔离，冲突风险高

## 搬运优先级速览

- 先搬（易、独立）：B、C、N、H(代理池/客户映射)、K(新增部分)
- 后搬（难、压在上游热点）：E(Claude)、F(Retry Override)、J(log.go)、L(billing/金额)

---

## A. 基础设施 / 环境隔离 — 🟢 后端已搬（5/5）

小范围环境/隔离改造，散落插桩于 main 与 common。

> 搬运进度(2026-10-28)：**后端 5 commit 全搬完**并编译+测试通过(rebase 分支)。
> - ✅ `2ff4b689e` redis key prefix:`REDIS_KEY_PREFIX` + go-redis v8 Hook(上游仍 v8,逐字移植 normalizeRedisKeyPrefix/redisKeyPrefixHook 等)+ redis_test。
> - ✅ `fea612d3b` debug 运行时开关:`DebugEnabled` 入 OptionMap init + updateOptionMap(common.DebugEnabled 已存在)。
> - ✅ `2d01b0591` node_name 入日志:上游两路(GenerateTextOtherInfo、ProcessChannelError)已统一走 `AppendRelayLogAdminInfo`,在其 `use_channel` 旁加 `other.SetAdmin("node_name", common.NodeName)`(typed LogOther,非 fork 的 raw map)。
> - ✅ `927dbc46f` 建用户默认 Key(后端):CreateUser `?default_token=true` 尽力建 name=default 无限额度 Key,回 `data.default_token_key`;对齐上游 Token 模型(明文 Key、AllowUserQuotaQuery=true)。前端延后。
> - ✅ `9f302fe45` cookie 隔离(**重表述**):上游弃 gin-contrib sessions、改用 `service.{RefreshCookieName,SessionHintCookieName}` 双 cookie。改为可选 `SESSION_COOKIE_PREFIX`——两 cookie 名 const→惰性 accessor 加前缀(避免 common↔service 环),默认空=原名逐字节不变;**OWASP:仅改名,HttpOnly/Secure/SameSite/Path/过期/签名全不动**。
> - ⚠️ 顺带修 M 回归:Prometheus 失败采样读 `relayInfo.ChannelId`(上游是从内嵌 `*ChannelMeta` 提升的字段,早失败路径 ChannelMeta=nil)会 panic——`TestRelayKeepAlive*` 暴露(pre-M 基线通过)。加 `ChannelMeta!=nil` 守卫(rebase `ede5da8f8`)。
> - 验证:`go build ./...` 仅 web/dist;relaykit `GOWORK=off` 通过;`go test ./common/ ./model/ ./service/ ./middleware/ ./controller/` 全绿。前端(建 Key 抽屉/node_name 详情/禁用二次确认)延后 web/src。

- 关键文件：`main.go`、`common/`、`model/log.go`、`controller/user.go`
- commits：
  - `343a6bc8b` add redis key prefix
  - `bf0357647` feat(cookie): cookie isolation
  - `716e31c44` feat(debug) add debug runtime switch
  - `ba84d516e` feat(log): add node_name into logs
  - `c28503305` feat(user): optionally create a default key when admin creates a user


## B. CSL-Hourly 数据接口 + RELAY_BASE_PATH — 🟢（7）

独立数据接口 + relay 路由白名单，基本自包含。

> 搬运进度（2026-10-27）：**全部 7 个 commit 已搬完**并编译+测试通过。核心 5 个先行；
> `dc203b7c8`(go.mod 本地 module 名) 的终态已在 C 模块解冲突时并入（import 改 `github.com/QuantumNous/new-api/third_party/...`、
> 去掉 baidu replace），cherry-pick 时该 commit 判定为已应用故 skip；`8c263d050`(OmniDataSearch reader)
> 在 C 的 `third_party/` 就位后补搬成功，测试 `TestGetCslHourlyFromOmni_*` 全绿。
> 冲突点均为 import/注册行：`router/api-router.go`、`router/relay-router.go`(strings)、`main.go`(base-path 包装改走 `router.WrapRelayBasePath`)。

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

> 搬运进度（2026-10-27）：`d5513f417`+`52ff5634a` 已 cherry-pick，编译+测试通过（`pkg/logshipper` 全绿、`model` 的 `TestShipLog*` 通过）。
> **关键事实：`third_party/csl-logshipper/` 不在 git 里**——`.gitignore` 忽略 `/third_party/`，由构建期脚本 `docker/sync-csl-logshipper.sh` 从外部私有库 `ai-gateway/csl-logshipper` 同步（该脚本本身也未入库）。
> 搬运时从主工作区把 `third_party/` 拷进 rebase 工作区供编译；**rebase 分支要能构建/部署，仍依赖同一套构建期 sync**。
> go.mod 冲突统一取上游 + `go mod tidy`（引入 `lumberjack`，当前解析为 v2.0.0）；Dockerfile 保留上游 relaykit/tokenkit ADD + 自定义 `COPY third_party/`。

- 关键文件：`pkg/logshipper/`(新增)、`model/log.go`(双写插桩)、`common/constants.go`+`common/init.go`(LOG_SHIPPER_* 配置)、Dockerfile、`.gitignore`

## D. Relay 稳定性 — 🟢 已解（配置化）

> 搬运进度(2026-10-28)：**不重放 fork 的全局注释改动**(会回退上游刻意的"断开即中止"+破坏其 `TestStreamScannerHandler_ClientCancel...` 测试+与 M3 shutdown 标记打架)。改为**按 (api-key, model) 维度可配开关**(仿 DEBUG/debug-scope 配置形式),把 D 的取舍交给配置:
> - 新增 `setting/operation_setting/client_disconnect_setting.go`:注册配置 `client_disconnect_setting`(Rules of {token_name, model})+ `AbortUpstreamOnClientDisconnect(tokenName, model)`,两字段都非空才匹配。
> - `relay/helper/stream_scanner.go`:主循环 client-gone case 用一个"未匹配即 nil channel(永不命中)"的通道武装。**默认(无规则)=保活上游读到底拿最终 usage 照实计费(防逃费)**;显式命中规则=立即中止上游(上游原行为)。shutdown 强取消仍走 upstreamCtx 独立。
> - 测试:原 ClientCancel 中止测试改为配规则走中止路径 + 新增"默认保活"测试;`client_disconnect_setting_test` 表测匹配器。build + `go test ./relay/helper/ ./setting/operation_setting/` 全绿。前端 System Behavior 编辑器延后 web/src。(rebase `a2a981bdd`)

- 关键文件：`relay/helper/stream_scanner.go`、`setting/operation_setting/client_disconnect_setting.go`
- 注意：上游已有同主题 fix(`42e05e013`,但取舍相反=断开即中止,有测试锁定);本实现把两种行为做成按 key+model 可配,默认取 fork 的保活语义
- commits：
  - `12bcbcc7e` fix(relay): keep upstream stream alive after client disconnect（fork 原始；未直接重放，改为配置化）

## E. Claude 链路定制 — ✅ 已搬（6/6）

Claude Code 客户端相关：metadata 注入、cache TTL、affinity、session 缓存。上游该区域改动密集，重点冲突区。

- 关键文件：`relay/claude_handler.go`、`dto/claude.go`（上游已迁至 `relaykit/dto/claude.go`）、`relay/channel/claude/relay-claude.go`
- commits（均已 cherry-pick 到 rebase 分支）：
  - `bfb763462` feat(claude): parse message_start string ✅
  - `cc2330e19` feat(affinity) add claude meta_data if not exist ✅
  - `482ceb577` feat(claude): remove meta_data.session_id ✅
  - `b83e80ddc` feat(claude): support force cache ttl 1h ✅
  - `c5a2192c7` feat(claude): add metadata injection switch & modify cache ttl option ✅
  - `04553bfc4` feat(claude): better metadata/cachettl option ✅
- 搬运要点：
  - 上游把 `dto/claude.go` 迁到独立模块 `relaykit/dto/claude.go`，`ClaudeMediaMessage.UnmarshalJSON` 里 `common.Unmarshal` 改为 `kitutil.Unmarshal`；`dto/claude_test.go` 相应迁到 `relaykit/dto/claude_test.go`（用 `kitutil.UnmarshalJsonStr`）。
  - `relay-claude.go` 与上游合并：保留上游 `info.ObserveResponseModel` / `maybeMarkClaudeRefusal(c, info, …)` 新签名，叠加自定义 `warnClaudeStringMessage`。
  - param-override 分支暂保留 `len(info.ParamOverride) > 0`，未引入 `info.PendingRetryRewrite`（属 F 模块，待 F 搬运时补）。
  - docs/*.md 与 `.my-rules/*.json` 未搬（聚焦代码，避免 docs 膨胀）。
  - 新增 `setting/operation_setting/group_model_rule.go`（原 extended_cache_ttl_setting.go 重命名）、`setting/model_setting/claude.go` 注入开关。
  - 验证：`go build ./...`（仅 main.go web/dist embed 因未构建前端报错，与 E 无关）、relaykit `GOWORK=off go build ./...`、`go test ./relay/ ./setting/operation_setting/ ./setting/model_setting/ ./relaykit/dto/` 全通过。

## F. Retry Override / Param Override（含命名规则集模板）— 🟢 后端已搬完（审计日志→J、前端→web/src 延后）

> 搬运进度（2026-10-28，关键发现）：上游已原生拥有 param-override（含审计器 paramOverrideAuditRecorder、channel_affinity 的 mergeChannelOverride）。你的 F 几乎只是'在其之上新增 retry-override'，唯一改了 param-override 自身逻辑的是 e680be3d8(fail-open 跳过坏操作，上游仍 fail-closed)。feature-tip 的 retry-override 比 inventory 的 22 commit 更完整(含 channel_pin 软绑定、override_template、retry_fallback、retry_body_guard 独立文件)。采用'最终态对齐'而非逐 commit。
> - ✅ Part1 核心引擎：relaykit/dto 增 RetryRule+字段；override.go 增 EvaluateConditions/applyPendingRetryRewrite 并让 ApplyParamOverrideWithRelayInfo 对所有格式应用暂存重写；relay_info 增 PendingRetryRewrite/RetryFallbackDone；新增 relay/retryrule 包。编译+retryrule 测试通过。(commit cb349e7ad)
> - ✅ Part2 命名模板库：override_template_setting + service/override_template(复用 mergeChannelOverride) + controller + GET /api/channel/override_templates。编译通过。(commit ffaec9e75)
> - ✅ Part3a 错误触发 retry-override 接线：channel_pin 软绑定 + CacheGetRandomSatisfiedChannel 优先 + relay 循环注入 maybeApplyRetryRuleFallback(same-channel 一次性/fallback 多跳)。编译+测试通过。(commit a1677191c)
> - ✅ Part3b 参数模板合并：distributor 加载渠道设置时合并 ParamOverrideTemplateRefs。(commit 43ea17efd)
> - ✅ fail-open(e680be3d8)：按 owner 决定保持 fail-open，已重新应用并把上游 override_test.go 的 fail-closed 断言翻改为 fail-open 契约(坏操作跳过+回滚+body 不变；return_error 仍阻断)。测试通过。(commit 64787cec8)
> - ✅ 200-body 守卫：retry_body_guard.go 接入 OpenaiHandler(非流式 unmarshal 前)与 OaiStreamHandler(precommitGuard 预提交缓冲)；无 retry_override 规则时守卫起始即 committed,零行为变化。(commit b1834741c)
> - 🟢 F 后端完成(6 commit)。延后项：审计写日志(model.RecordRetryOverrideLog)→J/logging 集群；前端 Override Rules UI/模板编辑器→web/src 重落;docs/.my-rules 丢弃。
> - ⏭️ 延后：审计/控制台日志(foldParamOverrideAuditLines 等)并入 J/logging 集群；前端 Override Rules UI 并入 web/src 重落；docs/.my-rules 丢弃。

最大模块。四阶段重试覆盖规则 + 共享求值器、200 中途限流检测、全格式接入、命名规则集模板、规则日志落库。

- 关键文件：`relay/common/override.go`、各 relay handler、`model/channel.go`、前端 Override Rules UI、`.my-rules/*.json`
- 注意：任务1 的"200 强制 fallback"已移除，勿改回
- 2026-10-29 增量（feature/custom-query-api）：phase4 动作裁决改为**首条命中生效**（原「任一命中为 fallback 则整体 fallback + phase3 跨规则叠加」已废弃）；新增第三种动作 `return_error`（终止重试、返回指定 `status_code`/`status_msg`，缺省 429）；`phase4_retry_action` 兼容字符串与对象 `{action,status_code,status_msg}`（`dto.RetryAction` 自定义 Marshal/Unmarshal，容错解析）。`CollectRewrites` 改返回 `retryrule.Decision`；`maybeApplyRetryRuleFallback` 返回 `(retry,terminate,detail)`，主循环 terminate 即 break。见 `docs/retry-override-design.md`。
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

> 搬运进度（2026-10-27）：**后端 4/5 已搬完**并编译+测试通过。`5a9e5e046`(客户批量模型映射)按用户要求**跳过**(暂不需要,且涉及 price.go 计费)。
> - `8b56e7433` 复制 behind-cdnaigw 自动建组/Key(后端 auto-merge)
> - `cafc9c1a6` 代理池:融合解法——`ResolveChannelProxy` 选出的 URL 喂给上游 `GetHttpClientWithProxySettings`,保住上游 HTTP/2 分片/协议/TLS 传输策略 + 叠加 httptrace 被动采样;放弃错误/消费日志记代理(依赖未迁移的 LogOther/J)与 upstream_request_id_header(属 J);dto 只加 `ProxyAliases`
> - `c127ed6fc` 代理池描述列(后端 proxy_setting 字段)
> - `c6ddffebe` 密钥掩码(GetChannel 掩码 + MaskKey/KeyMasked,mask 测试通过)
> **前端全部延后**:proxy-pool-section、copy-channel-dialog、channel-mutate-drawer 控件、channel-actions、i18n → web/src 重落(其中 drawer 是改上游共享组件,属较重的重落)。

- 关键文件：`controller/channel.go`、`model/channel.go`、`relay/channel/api_request.go`、`service/proxy_health.go`、`setting/system_setting/proxy_setting.go`、前端渠道/系统设置 feature

## I. 客户（Customer）维度 — 🟡 核心已搬(看板/前端待)

> 搬运进度(2026-10-28):✅ I 核心:model/customer.go(客户:下游ID→名称+前缀系列折扣,仅统计不计费)+ customer_cache.go(内存缓存+CustomerName/CustomerModelDiscount+SyncCustomerCache poller)+ controller/customer.go + /api/customer CRUD 路由 + AutoMigrate &Customer{}(主库)+ main.go poller。解锁 K 的按客户下钻。(8564445e8)
> ⏳ 待搬:客户金额看板(989ef4369 customer amount metrics、4abfc63d3 Customer Statistics)属 L/看板+前端;前端 customers 页。

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

## J. 消耗 / 错误日志增强 — 🟡 进行中（J-core 起步）

> 搬运进度（2026-10-28）：
> - 关键前提：上游已独立重建日志底座——`Log.Other` 改为强类型 `*LogOther`(model/log_other.go,三档可见性),且新增独立 `audit_logs` 表(model/audit_log.go,走 LOG_DB,原生支持 ClickHouse)。本 cluster 的日志写入须从旧 map 方案翻译到 `*LogOther`。
> - 决策①=A(owner 确认)：审计(含 F 延后的 retry/param-override)适配上游 `audit_logs` 表(RecordAuditLog),不再新增 LogType 行。owner 环境 LOG_DB=ClickHouse,audit_logs 自动入 ClickHouse。
> - ClickHouse 注意：logs.session_id / logs.error_signature_hash / quota_data.customer_id 真列在 LOG_DB(=ClickHouse),加列须照上游 ClickHouse 迁移分支写法(显式 CREATE/ALTER),非 GORM AutoMigrate。
> - ✅ J-core 步骤1：session_id 解析器 relay/session_id.go + 两个 ContextKey,带单测。(commit 4610a52ff)
> - ✅ J-core 步骤2a：session_id 写入消耗日志 other(LogOther public)；claude/compatible/responses 三处 handler 已调 recordRequestSessionID；全后端编译通过。(commit 4b9ae809e)
> - ✅ J-core 步骤2b(代码)：Log.SessionID 真列+索引；RecordConsumeLog/RecordErrorLog 从 ctx 读 session_id 双写(列+other)；ClickHouse logs 表 CREATE 加列 + 幂等 ALTER;SQLite/MySQL/PG 走 AutoMigrate。编译+model/relay 测试通过。(commit 758e1b133) ⚠️ **DB 迁移未经真实库验证(本地无实例),待 owner 在含 ClickHouse 的环境跑迁移验证。**
> - ✅ J-core 步骤2c：session 搜索改为'同步索引过滤'(owner 决定)——session_id 已是索引列,无需异步扫描。LogFilter 加 SessionId + apply `logs.session_id = ?`;controller 读 c.Query("session_id")。**pkg/logscan 引擎 + service/session_log_search 不搬(索引列下已过时)。**(commit 371a13619)
> - ✅ 顺带修两处既有回归(go test ./controller/ 暴露):F 的 logger.LogDebug 非常量格式串(vet)、H 的 key_masked 字段未分类(channel_authz)。(commit 501a1b834)
> - ✅ J trace-tag：common/trace_tag.go 解码 x-bce-aigw-trace-tag(SIV/AES/HKDF,env AIGW_TRACE_TAG_KEY) → downstream_request_id + customer_id。按 owner 要求**升级为真索引列双写**(raw+ids 另存 other admin),LogFilter 同步索引等值过滤(取代 fork 的 LIKE-on-other);ClickHouse CREATE+ALTER。(commit ea0050ce6) ⚠️ 同样待真实 DB 迁移验证。
> - ✅ J 过滤(后端子集):status_code(error-log other JSON LIKE,跨库含 ClickHouse) + error_keyword(logs.content 子串 LIKE,按库转义) 作为 LogFilter 字段;controller 读 status_code/error_keyword。(commit fb6d0b324)
> - ⏳ J 剩余(延后):include/exclude、多值过滤、net 生成速度、月度消耗汇总——前端耦合/统计类,随前端重落或后续单独做。
> - 🟢 J 后端主体(session_id/trace-tag 真列+搜索、status/error 过滤)已搬;schema 变更(session_id/downstream_request_id/customer_id 三列)待真实 DB 验证。
> - ⏳ 后续：J-logscan 引擎→session 搜索→trace-tag→多维过滤;K 错误统计;I 客户;L 金额口径;M 余下。前端全部延后 web/src。

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

## K. 明细错误统计（Error Stats）— 🟡 进行中(模型层已搬,API 待 I)

> 搬运进度(2026-10-28):
> - ✅ K步1:service/error_signature.go(规范化+sha256 哈希+默认规则+Option 懒加载)+ relay_error.go 落签名到 other(admin)+ Log.error_signature_hash 真列双写 + LogFilter 索引过滤 + ClickHouse CREATE/ALTER。(86f1a78dc) ⚠️列进待验证迁移批次。
> - ✅ K步2模型层:model/error_stats.go(聚合:按 signature/short-name/attribution 分组)+ model/error_signature_alias.go(hash→短名/归因/备注+内存缓存)+ model/log.go 补 otherFieldLike/Absent/customerNoneFilter 跨库 helper。(f6e0b5d88)
> - ✅ K步3:controller/error_stats.go + 7 条路由(/api/log/error-stats、error-signature-rules(+suggestions)、error-signature-aliases)。由 I 解锁(model.CustomerName)。K 后端完成(除延后项)。(d9b9e0356)
> - ⏭️ 延后:Prometheus 错误指标(需 pkg/prom_metrics,属 M);前端 error-stats feature。

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

## L. 数据看板 / 金额口径 / 计费 — 🟡 后端核心已搬(前端/看板待)

> 搬运进度(2026-10-28):已读 billing.md 全文(门控)。**L 核心后端已按方案A落地**(rebase 分支 4 commit)。
> - ✅ bb824d839:SaveQuotaDataCache 暴露 quota_data 写失败(observability-only,扣费/去重语义不变)。(782e13471)
> - ✅ aacae6d89 允许无价模型全局开关 `AllowUnsetModelRatio`(operation_setting+option+price.go 两处 missing-ratio OR 进 acceptUnsetRatio)。(21b71ee6c)
> - ✅ 50d06f7d9 渠道 StatGroupRatio:`*float64` 加到 **relaykit/dto/channel_settings.go**(独立模块,GOWORK=off 编译通过);text_quota 读 `ChannelSetting.StatGroupRatio`(nil/<=0/Inf/NaN→1)。(f82a1edb3)
> - ✅ bd028db5e 三口径金额(**方案A**):把内联计费算术抽成 `finalizeTextQuotaForGroupRatio(summary, priceData, scaledBase, surchargeUnit, groupRatio, isRatioBranch)`——billed 用 `summary.GroupRatio` 调(**逐位等于原扣费**),RawQuota@1 / StatQuota@StatGroupRatio / CustomerStatQuota@CustomerDiscount 另三调;tiered 路径用 `tieredCaliberQuota`=`ActualQuotaBeforeGroup*altRatio(+surcharge)`(**比 fork 多补了 tiered 口径,fork 原来不重算**)。口径走 `other.SetAdmin`(root 全可见),并提成 logs 真列 `raw_quota/stat_quota/stat_group_ratio/customer_stat_quota/customer_discount` 双写 + logshipper.Row 映射(入 ClickHouse);quota_data 加 `channel_stat_quota/customer_stat_quota` 列 + 聚合(GetAllQuotaDates/Flow/GroupByUser/Customer)。扣费不变量回归测试 `TestTextQuotaCalibersPreserveBilledDeduction`(ratio/价格/tiered/groupRatio=0/缓存/audio)已绿。(0f9fa3796)
> - ✅ 62ff1f022 后端:`retry_triggered` 真列(index)+ LogFilter(`*bool` 索引过滤)+ error 日志写入;customer_discount/fallback 口径已并入上条。(9272210e7)
> - 关键不变量**已验证**:billed `Quota` 逐位不变(抽取仅重排 decimal 乘法顺序,shopspring/decimal 精确,Div 位置不变;surchargeUnit*gr==ToolCallSurchargeQuota、audioInputUnit*gr==audioInputQuota 均分配律成立)。口径仅上报,从不进 UpdateUserUsedQuota/UpdateChannelUsedQuota/SettleBilling/Log.Quota。
> - 验证:relaykit `GOWORK=off go build ./...` 通过;root `go build ./...` 仅 main.go web/dist embed 报错(前端未构建,预期);`go test ./service/... ./model/...` 全绿;`relay/helper` 仅 `TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns` 失败——**上游既有 stream-scanner 计时 flake(base 782e13471 同样失败,与 price.go 4 行改动无关)**。
> - ⚠️ **DB 迁移未经真实库验证**(本地无 MySQL/PG/ClickHouse 实例):logs 5 列 + retry_triggered + quota_data 2 列,主库走 AutoMigrate(SQLite 已验),日志库/ClickHouse 走显式 CREATE + 幂等 `ALTER TABLE logs ADD COLUMN IF NOT EXISTS`。待 owner 在含 ClickHouse 环境跑迁移验证。
> - ⏳ 延后:前端 dashboard 三口径切换/金额卡/分流 tooltip、usage-logs 列与过滤 UI → web/src 重落;Prometheus `RecordStatAmounts`/`RecordRelay` 口径指标 → 属 M(pkg/prom_metrics 本仓不存在)。

三口径金额（原始/渠道折后/客户折后）、分流、渠道统计倍率、允许无价模型。正面压在上游 billing 重构上。

- 关键文件：`service/text_quota.go`、`relay/helper/price.go`、`types/price_data.go`、quota_data、前端 dashboard feature
- commits：
  - `bd028db5e` feat(dashboard/logs): stat-amount calibers across dashboard + log column dual-write
  - `bb060e5e8` feat(dashboard): expand flow tooltip metrics
  - `bb824d839` fix(dashboard): surface quota_data write failures instead of swallowing them
  - `50d06f7d9` feat(billing): add channel statistics group ratio for raw/discounted amounts
  - `aacae6d89` feat(billing): add global switch to allow models without configured price
  - `62ff1f022` feat(logs): track discount fallback and retry overrides

## M. 性能监控 / 过载保护 / 系统设置 — 🟡 进行中（上游比对后搬）

> 搬运进度(2026-10-28，已做 upstream 比对):
> - **已在上游、跳过/对齐**:性能监控vs过载保护分离(`1d68af68f`→上游 `5e71ef98d`+`#7317`,cgroup 口径+连续性/新鲜度)、进程级指标面板(`766a782ac`→上游 `bbebb6f3e` 同名,`process_monitor`/`http_runtime_stats`/`system_instance` 全在;若需 goroutine/heap/GC 字段再补)。
> - **✅ Prometheus 导出(`53d65ae78`)**:pkg/prom_metrics(独立 registry/懒加载/禁用即 no-op)+ middleware/prometheus.go(按路由模板采样)+ main.go `ENABLE_METRICS` 开关 + 独立 `METRICS_PORT`(默认9090)mux + `/metrics`。成功样本挂结算点(text_quota/quota,精确 tokens/quota),失败样本挂请求边界 defer(controller/relay.go);**未复用上游已删的 quota.go perf_metrics 采样点**(prom 是独立子系统)。go.mod client_golang 转直接依赖。build+vet+service/middleware 测试通过。(rebase `0be7160ac`) ⚠ `/metrics` 独立端口**默认无鉴权**,内网部署需防火墙限制 METRICS_PORT 或绑 localhost。
> - **✅ 过载拒绝落日志(`0c6732192`)**:上游 `checkSystemPerformance()` 返回 503 时(`constant.ErrorLogEnabled && types.IsRecordErrorLog`),用 `*LogOther` 写一条错误日志:public(request_path/error_type/error_code/status_code)+ admin(error_signature/error_signature_hash 走 K 约定使列双写、node_name、system_overload{cpu/mem/disk usage+threshold,authenticated=false});`RecordErrorLog(c,0,0,...)` 预鉴权无归因;并给 RecordErrorLog 加 `userId>0` 守卫避免空查用户设置。中间件加 `systemStatusProvider` 测试seam(生产行为不变)。`prommetrics.RecordError` 跳过(属 K 错误指标,延后)。build+vet+middleware/model/service 测试通过。(rebase `04aff3e4f`)
> - **✅ 进程退出中断标记(`d5009da15`)**:新增 `common/relay_shutdown.go`(RelayRequests 注册表:`Begin`用`WithCancelCause(WithoutCancel(ctx))`脱离客户端、`Drain`/`ForceCancel(info)`广播`*ProcessShutdownInfo`为cancel cause/`Wait`/`RelayUpstreamContext`/`ProcessShutdownFromContext`/`TrackRelayCloser`)+ `middleware/relay_shutdown.go`(drain中新请求→503 server_shutdown;否则换ctx+追踪body closer)+ main.go 叠在上游优雅关闭上(Drain→Shutdown→Wait,超时则ForceCancel+Close)+ router 仅注册到 relay 组 + relaykit/types `ErrorCodeProcessShutdown`。commit1=机制(08ade7240),commit2=日志标记(15061f56a)。标记走 `*LogOther` admin 档 `process_shutdown`(model/log.go helper,消耗&错误日志;**扣费路径未碰**),重试抑制+跳过渠道封禁在 `controller/relay.go`+`service/relay_error.go.ProcessChannelError`。
>   - **关键适配**:上游 stream_scanner 的 ClientGone 处理与 AWS/vertex 的 client-cancel 测试均已漂移——**client 取消→仍 ClientGone,仅 shutdown 强取消→shutdown 标记**;AWS 保留 client-cancel 中止(用 `context.AfterFunc` 传播 shutdown 而非粗暴 detach);`api_request` 仅 DoApiRequest/DoFormRequest 改走 upstream ctx,newTaskAPIRequest 留 client ctx。**顺手修好了上游本就红的 `TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns`**(此前误判为 flake,实为基线真·失败)。
>   - 验证:build 仅 web/dist;relaykit `GOWORK=off` 通过;`go test ./relay/... ./common/ ./middleware/ ./model/ ./service/` 全绿,含 ClientGone/ProcessShutdown/AWS-cancel 具名测试。
> - **待搬/延后**:Docker CHANGELOG(`5b87e192b`)——**孤儿,暂不搬**(上游无根 CHANGELOG.md,消费方=延后的前端维护页,留待前端重落时一起做);System Behavior 配置+跨实例即时 pub/sub(`12b3ea827`)——配置区属前端;pub/sub 是对现有轮询的增量,按上游 `wsmanager`/`user_auth_cache` 范式重做(可选)。
> - **部分已有、按上游范式重做**:System Behavior 配置 + 跨实例缓存即时 pub/sub(`12b3ea827`,上游现为定时轮询;复用 `pkg/wsmanager`/`user_auth_cache` pub/sub 范式)。
> - **前端延后(web/src)**:维护页版本/changelog(`55830f6be`)、System Behavior 区、禁用二次确认(`bd2f30822`)。

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

> 搬运进度（2026-10-27）：**后端已搬完**并编译+测试通过（`service` 的 `TestExtractSchedulePlaceholders`/`TestResolveSchedulePromQL`/`TestPreviewSchedulePromQLReducesRows` 全绿；`/api/schedule/*` 路由已注册，admin-only）。
> model/main.go 冲突解法：X-schedule 3 张表加入上游标准 AutoMigrate 列表；**丢弃了原 commit 顺带带的 `migrateDBFast()` 自定义并发迁移函数**（属更早的未迁移基建、上游无、且无调用者）。
> **前端已延后**：`web/default/src/.../scheduling/*` + section-registry + i18n 落在旧目录,未并入上游 `web/src/`；连同其它模块的前端一起进入"前端重落 web/src"阶段。CHANGELOG.md 保持上游(已删)。

- 关键文件：`controller/x_schedule.go`、`model/x_schedule.go`、`service/x_schedule.go`、`router/api-router.go`(路由)、3 张 `x_` 前缀表

---

## ⚠️ 前端重落（web/default → web/src）

上游删掉 `web/classic`、把 default 主题提升为 `web/src/`(即旧 `web/default/src/` 原地上移一层,`@/*`→`./src/*`)。所以前端搬运是**路径平移**,不是重写。

**已验证的搬运法(2026-10-27 用 N 试点跑通):**
1. 纯新增组件：`cp -R web/default/src/<path>` → `web/src/<path>`(同名路径)
2. 改共享文件(section-registry 等)：把定制条目手工加到上游 `web/src/` 对应文件(上游该文件结构可能已漂移,按新结构加,别照搬旧 diff)
3. `bun install` → `bunx tsc -b` + `bunx oxlint` 定点修漂移
4. i18n：组件用英文源串作 key,**英文天然可用**;非英文翻译用 `bun run i18n:sync` + i18n-translate skill 或从原 commit 搬译文

> **已踩的漂移坑(I/K 实证,2026-10-28):**
> - **路由树**:加新顶层路由后须 `bun run build` 一次重生成 `src/routeTree.gen.ts`(TanStack router rsbuild 插件在 build 时生成,`bunx tsc -b` 不会生成)。section-only 模块(如 N)无此需求。
> - **类型检查命令**:`bun check -b`(package.json typecheck 脚本)在本地无 "check" 子命令会报错;直接用 `bunx tsc -b`(解析到 TS 7.0.2 native preview)。
> - **oxlint 全仓有既有 error**(scripts/*.mjs 等),须 `bunx oxlint <改动路径>` 定点跑,别看全仓汇总。
> - **共享 `Combobox` 漂移**:上游 `LegacyComboboxProps` 去掉了 `clearable` prop;所有带 `options=` + `clearable` 的旧调用会 fall through 到泛型重载报 TS2769,删掉 `clearable` 即可。
> - **端点漂移**:fork 前端 api.ts 的路径可能对不上 rebase 后端我自己搬的路由(如 K 的 `/api/error-alias`→`/api/log/error-signature-aliases`),逐一核对后端 router 实际注册的路径。
> - **跨模块路由搜索参**:某模块下钻到另一模块页面时,目标路由 `validateSearch` schema 必须含该搜索参否则 `buildLocation`/navigate type-check 失败;缺失的先按"接受但忽略"补 optional 字段(等对应模块真正消费)。

> **前端重落清单（下一会话专项）——后端均已就绪,前端全部待平移到 `web/src/`:**
- **N 自动调度** ✅ 前端已重落(`211efd036`):scheduling 面板 11 文件平移、section-registry 接回;tsc -b + oxlint 全 0;非英文译文待补(英文已可用)
- **I 客户** ✅ 前端已重落(`aabbbbe88`):features/customers/* 平移 + `/customers` 路由 + 侧栏(Contact 图标,Admin 组)。漂移修复:上游 Combobox LegacyComboboxProps 去掉了 `clearable` prop,customer-filter-combobox 去掉该 prop。tsc -b + oxlint(改动文件)全 0;非英文译文待补
- **K 错误统计** ✅ 前端已重落(`8e478c63b`):features/error-stats/* 平移 + `/error-stats` 路由 + 侧栏(AlertTriangle,admin)。漂移修复:(1)api.ts 别名端点对齐后端 d9b9e0356 的 `/api/log/error-signature-aliases`(原 fork 用 `/api/error-alias`);(2)filter-bar 的 5 处 Combobox 去 `clearable`;(3)usage-logs/$section 路由 schema 加 `errorSignatureHash/errorSignature` 可选搜索参(K 下钻在新标签打开过滤后的 usage-logs,需 type-check;**J 落地其 error-signature 过滤前先接受但忽略**)。依赖 I(共享 customer-filter-combobox)。tsc -b + oxlint 全 0;非英文译文待补
- **H 渠道增强 + F retry-rules** ✅ (`2cf282eed`):channels/* 并进上游重写的 drawer——代理别名多选、密钥掩码(editing admin 见掩码,super-admin 揭示/复制走 2FA)、复制 behind-cdnaigw 自动建组/Key(`/api/channel/copy_behind_cdnaigw/:id`)、retry-rules-section(F)。getProxyHealth 本地定义避免依赖未搬的 system-settings proxy 模块
- **L 金额口径** ✅ (`a45b58fff`):dashboard 三口径(raw/渠道/客户)选择器+金额卡(consumption-amount-cards)、分流 Sankey 折后宽度+tooltip、用户分析金额切换;**跳过「客户统计」section(customer-charts)——依赖未搬后端(quota_data.customer_id 列 + /api/data/customers),属 I 看板延后项**
- **J 日志增强 + L 金额列** ✅ (`50ce7c29a`):common-logs 过滤 session_id/downstream_request_id(admin)/status_code/error_keyword(后端 LogFilter 已验)、K 下钻 error_signature_hash 真消费、详情 Session/Downstream 行、三口径金额列(读 logs 真列)、net 速度(客户端)。**跳过(后端缺):异步 session 搜索、include/exclude·多值过滤(后端读单标量)、月度汇总/usage-statistics 原型、customer-name 过滤/raw trace-tag**
- **M 性能/系统 + D + E + L开关** ✅ (`ccb429d37` 系统设置 + `3ba2addf3` system-info):override-templates(F)、proxy-pool-section、group-model rules(E=extended_cache_ttl+metadata_injection 注入)、client_disconnect 规则编辑器(D)、AllowUnsetModelRatio+DebugEnabled 开关(System Behavior)、sensitive-words、进程指标面板。**跳过:debug-scope 按(key,model)规则(后端只有全局 DebugEnabled)、changelog/维护页版本(延后,无端点)、routing-reliability(上游已换 request-policies)**
- **A 基础设施** ✅ (`9c368cac8` 禁用二次确认 + `25907449b` 其余):建用户默认 Key(`?default_token`→data.default_token_key)抽屉+一次性密钥揭示、node_name 日志详情行(admin)、Key 禁用二次确认。option 默认种子上游已搬到 system-settings/models/index.tsx
- **E Claude** ✅:metadata 注入开关/cache-ttl 的 group-model rules UI 已随 M(`ccb429d37`)落地(extended_cache_ttl_setting + metadata_injection_disable_setting 编辑器)

> **前端收口(2026-10-28):全部模块已重落 `web/src/`**,rebase 分支新增 8 个前端 commit(I/K/keys-A/H+F/L/J+L/M+D+E+F/system-info/A)。`bunx tsc -b` 全绿 + `bun run build` 成功(17.5s) + 各改动文件 oxlint 0 error。**非英文译文统一待补**(英文源串天然可用)。**后端缺口导致的前端延后项**(见各模块"跳过"):客户统计看板、日志 include/exclude·多值·月度汇总·net统计、debug-scope 规则、维护页 changelog——均因对应后端未搬;需先补后端再回这些前端。**运行时未实测**(本地无跑起来)。

> **增量搬运(2026-10-29)——feature 分支快照后新增的提交补搬:**
> - **eda601c**(debug scope by api-key+model)✅ 全搬(`353e200a6`,后端+前端):debug_scope_setting.go(+test)、logger.DebugActive(全局 DebugEnabled 或按 ctx token_name+original model 命中作用域)、3 处 relay 调试日志 guard 换 `logger.DebugActive(c)`(relay-openai/compatible_handler/price.go,纯调试可见性,**非计费逻辑**,price.go 的 billing 门禁已读 billing.md 清);前端 debug-scope-rules-field + System Behavior 接线。gofmt+go test(operation_setting/logger) 过、tsc/oxlint 过。
> - **558a4f1**(merge stat scans + lazy badges + smaller page)✅ 前端部分搬(`15e7738ef`):common-logs-stats 用 requestIdleCallback 懒加载 badges、桌面默认页 100→30。**后端 Part A(合并 stat 扫描)跳过**——rebase 的 GetLogsStat/SumUsedQuota 没有 fork 要合并的第二个"amounts"扫描(L 金额口径在 rebase 走的是真列,不在这条 stat 路径),无可合并、强搬会动统计语义,故按保守原则跳过。
> - **05e49d3**("some thing")跳过:只含 `.my-rules/*.json` + `docs/*.md`(文档/本地规则,非代码,按既定政策不搬)。
> - ⚠️ **发现 rebase 既有红测(与本次无关)**:`TestOaiResponsesStreamHandlerCompletionLifecycle`(relay/channel/openai,responses 流式计费生命周期:disconnect/cancel/midstream 等子用例)在干净 HEAD(25907449b,stash 掉本次改动后)同样 FAIL,每子用例约 1.00s(疑超时/ctx deadline)。**非本次搬运引入**,待单独排查(可能与 D 客户端断开/M shutdown 的 responses 计费交互,或上游该测试本就需额外环境)。

---

## 搬运执行建议

1. 从当前 `upstream/main` 拉新分支作基底（自带前端新结构 + 497 commit 修复，含 CVE/OOM/Claude/billing 修正）。
2. 按 B → C → N → H → K → I → M → A → D → E → F → J → L 顺序搬（先独立后侵入）。
3. 侵入模块（🔴）一律改写为"独立文件 + 上游文件插一行 hook"，每搬完一个模块 `gofmt && go test`。
4. 前端（E/F/H/I/J/K/L/M/N 的 UI 部分）需在上游新的 `web/src/` 结构重落，无法平移 `web/default/`。
5. L（金额口径）与上游 `cc/cc1h/cc12h` 表达式引擎冲突最大，建议最后搬并单独评估。




