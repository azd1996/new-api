# 更新日志 / Changelog

本文件记录 new-api 网关（本 fork，分支 `feature/custom-query-api`）在上游之上的自定义变更。
格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。详细设计见 `docs/` 下对应文档。

> 说明：本文件在前端构建时被打包进「系统设置 → 运维 → 维护」页。修改后需重新执行 `bun run build` 才会在页面生效。

## [2026-11-05]

### 修复 Fixed

- **对象配置读取与前端类型检查**：通用设置页支持读取对象型 JSON 配置，修复代理池 TTFT 配置被当作字符串返回的问题；无效对象配置回退默认值。区分 TTFT 表单输入与校验后的数字类型，补齐渠道列表测试数据缺失的资源类型字段；WebSocket 请求测试夹具补齐 i18n 初始化，避免单独运行鉴权拒绝用例时崩溃。
- **会话缓存诊断识别断点未推进，并纠正稳定历史损失归因**：对话历史持续增长、缓存断点却停在早期位置时，新增「断点覆盖不足 / 未推进」原因，展示影响尝试数与未覆盖稳定历史的估算 Token 量。原「断点调整后」明确为「尾部断点修正后」，另增独立的「推进后模拟命中率」和「推进 + 1 小时 TTL 模拟命中率」；滚动模拟遵守最多 4 个断点、20 块回看、缓存最小长度、TTL 与请求完成时间约束。修正本日此前「稳定历史兜底归上游差额」的过度归因：字节哈希相同不代表历史曾被断点覆盖或缓存仍可读，覆盖不足归 `breakpoint_stalled`，剩余原因未明的稳定历史归 `stable_unexplained`，不再直接归上游。同一尝试同一原因的多个损失片段合并，影响尝试数只计一次。已有骨架查询时重算，无需重新采集；不改变骨架表结构、在线请求或计费。模拟结果仍是按字节比例估算，不承诺实际收益；OpenAI 保持原有展示，旧接口缺少新增字段时显示「—」。实现见 `model/cache_skeleton_analysis.go`、`model/cache_skeleton_loss.go` 和使用日志会话诊断组件；详细口径见 `.comate/specs/cache-breakpoint-skeleton/doc.md` §7.6.4、§7.12、附录 D。
- **修复会话范围缓存汇总与来源分项口径不一致**：范围顶部的实际命中率改为只统计当前筛选范围内的 consume 日志，`content-fallback` 与非内容来源分项使用同一批日志的 `cache_read / input_all` 分子、分母并严格守恒；请求数仍可包含错误日志，避免将输出 Token 或时间窗外的会话生命周期数据混入命中率。骨架仅用于历史 warmup，只有能按 request/session/node/token/channel/`relay_attempt` 精确匹配的 consume 尝试进入模拟、损失与覆盖率样本，并展示可分析样本覆盖率；每个会话最多回放 2000 条历史骨架，截断时标记为 partial。新日志在 `other.admin_info.relay_attempt` 记录从 0 开始的重试序号；旧日志没有该字段时仍可统计实际命中率和来源分项，但不会进入精确模拟样本。实现见 `model/cache_skeleton_analysis.go`、`service/log_info_generate.go`、`controller/log.go` 和使用日志会话诊断组件；不改变数据库 schema、在线请求或计费。

### 新增 Added

- **behind-cdnaigw 标准/非标渠道分流**：保留一个 API Key、一个分组，通过名称严格以 `-非标` 结尾的渠道承接非标请求。复用现有会话亲和规则及键提取判断客户端原始请求，分类不依赖亲和缓存命中、绑定总开关或 SessionMode，且不受后续请求改写影响。「系统设置 → 模型 → 分组」新增分组级开关，仅作用于 `behind-cdnaigw_` 前缀的实际分组；客户编辑页新增客户×模型前缀配置，未命中前缀默认关闭，最长前缀优先。默认自动，无启用的非标渠道或显式关闭时直接走标准渠道，不消耗重试次数。非标故障仅在现有重试预算与策略允许时回退标准渠道，保留 strict、指定渠道等限制；WebSocket 后续请求类别与已锁定渠道冲突时要求重连。behind 渠道新增复制快捷入口，目标完整名称严格等于源名称加 `-非标` 时只复制渠道、不变更分组或 Key，支持 50 字节源名加 7 字节后缀；其他名称保留原专用复制流程。方案见 `docs/behind-cdnaigw-session-routing-plan.md`。
- **会话列表新增「筛选时间范围内 Token 总量」排序维度**：此前会话列表的「Total Tokens」统计的是该会话的**生命周期总量**（不随筛选时间范围变化），无法单独按「这段时间内」的活跃程度排序。现 `GetLogSessions` 额外按当前筛选的起止时间聚合每个会话在该范围内的 `prompt_tokens + completion_tokens`（范围之外的 `logs` 行不计入），新增列 `range_total_tokens` 与可排序维度 `range_total_tokens`（`logSessionSortColumns`），与既有的生命周期 `total_tokens` 互不影响、各自独立展示与排序。前端会话列表新增对应列（显示 token 数及其占「筛选范围内总量」`range_total_tokens_sum` 的百分比）。实现见 `controller/log.go`（`respondLogSessions`/`GetLogSessions`/`GetUserLogSessions`）、`model/log_session.go`（`logSessionQuery`/`GetLogSessions`/`LogSessionSortOptions`）；前端 `web/src/features/usage-logs/components/usage-logs-sessions.tsx`、`columns/session-columns.tsx`、`types.ts`；回归用例见 `model/log_filter_test.go`。
- **会话缓存汇总新增「Claude 内容兜底 vs 非内容兜底」来源对比表**：此前「会话 ID 来源」只能在通用日志按单一取值筛选（`metadata.user_id` / `content-fallback` / `prompt_cache_key` 等五种），无法直观对比「靠内容回退推断出会话 ID」（`content-fallback`，即客户端既没显式带 `metadata.user_id` 也没带可识别的会话标识，网关按内容相似度猜测会话边界）与其余四种「有明确标识来源」的会话在缓存命中率上的差异——内容回退推断的会话边界本身就不如显式标识可靠，命中率差异可以作为佐证。现会话缓存汇总新增一张小表，按 `session_id_source = content-fallback` 与否两组，分别统计会话数、请求数、token 总量与聚合缓存命中率（`cache_read / input_all`）。接口在 `GET /api/log/cache-skeleton/session` 响应新增 `source_stats` 字段。实现见 `model/cache_skeleton_analysis.go`（`getSessionSourceStats`/`SessionSourceStats`）、`controller/log.go`；前端 `web/src/features/usage-logs/components/usage-logs-sessions.tsx`、`types.ts`。
- **参数覆盖 `return_error` 支持自定义响应头（Param Override Response Headers）**：此前 `return_error` 操作只能控制状态码、`message`/`code`/`type` 等错误体字段，无法附加自定义响应头，下游若想按响应头识别「被网关拦截」及具体原因（例如区分「被禁用工具拦截」与普通上游错误、附带 `Retry-After`）做不到。现 `return_error` 的 `value` 新增可选对象字段 `response_headers`（键值均为字符串，不支持数组/多行/变量插值），解析时校验头名称与值的合法性、禁止覆盖 `Content-Length`/`Transfer-Encoding`/`Connection`/`Keep-Alive`/`Trailer`/`Upgrade`/`Proxy-Connection`/`Content-Type`/`Content-Encoding` 等由响应格式或 HTTP 传输层管理的头、禁止重复头名（大小写不敏感）；配置非法时按参数覆盖引擎既有行为跳过整个操作并记录日志，不会半生效。响应头只在该错误**作为最终响应返回**时才写入：如果后续重试成功，不会残留在成功响应上；如果 Claude 流式心跳已提交响应头（SSE 已开始），则无法再追加 HTTP 响应头，错误仍通过当前流返回。`controller/relay.go` 原本内联在 `defer` 里的错误响应分支抽成独立函数 `writeRelayError`，统一处理三种中继格式（OpenAI / Claude / Realtime）与响应头写入时机。实现见 `relay/common/override.go`（`ParamOverrideReturnError.ResponseHeaders`/解析与校验）、`relaykit/types/error.go`（`NewAPIError.ResponseHeaders`）、`controller/relay.go`（`writeRelayError`）；配置示例与约束说明见 `.my-rules/param-override-error-response-headers.md`；回归用例见 `controller/relay_stream_test.go`（`TestParamOverrideResponseHeaders`/`TestParamOverrideResponseHeaderValidation`）。

### 修复 Fixed
- **缓存损失归因：哈希未变的旧历史前缀被误记为「新内容」**：客户反馈会话诊断里绝大部分未命中都被归到 `new_content`，但实际观察到命中率波动和上游渠道内部账号池是否稳定高度相关（账号池出现死号被替换/补号时，命中率从 80%+ 跌到 60%~70%），怀疑诊断结论低估了上游因素。排查发现 `skeletonTurnLosses` 在计算「旧历史范围内哪些块哈希没变」（`stable_tokens`）时，此前只在检测到部分内容分叉时才使用这个值去 clamp 结构变化那一步的下限；如果旧历史范围内的块哈希**完全没变**，这段计算被整个跳过，`stable_tokens` 从未被单独拿去检查是否已经被瀑布前面的候选项覆盖。当模拟器自身的命中预测（`current`/`adjusted`/`adjusted1h`/`lookback_exceeded` 等候选项）因固定 20 块回看窗口或断点 TTL 的建模偏差而判断不出预期命中时，这段哈希证实没变的差额会连同真正的新增内容一起，全部滑进瀑布最后一步的兜底项 `new_content`——这个机制和渠道、Key 是否变化完全无关，即使网关配置的渠道/Key 全程不变（只是上游在那个 Key 背后做了账号轮换），诊断结果也会显示成「新内容」而非「上游未命中」，掩盖真实的上游因素。现 `stable_tokens` 改为无条件计算，并在瀑布走完所有模拟器候选项之后、落入 `new_content` 之前单独 clamp 一次：若覆盖量还没推进到 `stable_tokens`，差额计入第一步已判定好的标签（`channel_switch` 或 `upstream_miss`），不再放任它漏到兜底项；真正发生分叉的段落（`param_changed`/`prefix_changed`）逻辑不变，仍建立在这一步 clamp 之后的覆盖量上，token 账本不重不漏。此前把这类差额算作 `new_content` 的历史诊断记录在重新查询时会按新逻辑重算，不需要重新采集；不改变 ClickHouse 骨架表结构、不改变实际发往上游的请求、不改变计费。实现见 `model/cache_skeleton_loss.go`（`skeletonTurnLosses`）；回归用例见 `model/cache_skeleton_test.go`（修正既有用例 `prefix_change_excludes_stable_prefix_and_new_blocks` 的错误预期值，新增 `TestClaudeCacheLossStablePrefixSurvivesExhaustedSimulatorCeilings` 复现前缀哈希完全没变但间隔超过所有模拟器候选项窗口的场景）；详细机制与分解顺序见 `.comate/specs/cache-breakpoint-skeleton/doc.md` §7.6.4、§7.11。

## [2026-11-04]

### 新增 Added
- **渠道请求体字段改写（Request Key Rename）**：渠道设置新增 `request_key_rename` 规则（`{path, keys}`，支持引用「系统设置 → 模型」新增的共享模板库，并可限定生效的模型名单），用于把 relay DTO 不认识、此前解析时会被直接丢弃的客户端字段改写成网关认识的名字（例如把 Gemini 下游传来的 snake_case `file_data` 改成网关 DTO 期望的写法）。改写在**单次遍历**里完成，只改 key、原样保留值、键顺序与空白；若没有任何规则命中该请求体则直接返回原文，不产生拷贝。按渠道选中的规则在**每次尝试（含重试）前**应用，并在渠道选择前与每次重试前把请求体还原为客户端原始内容，避免规则跨渠道串用；改写后的请求体会重新跑一遍 `GetAndValidateRequest` 校验，`max_tokens`、图片张数等上限不受影响。命中的改写记录在消耗/错误日志的 `other.request_key_rename`，新增对应日志类型筛选和管理员详情展示行。实现见 `relay/common/request_key_rename.go`、`controller/request_key_rename.go`、`relaykit/dto/channel_settings.go`、`model/log.go`、`service/override_template.go`；前端渠道编辑抽屉的 JSON 字段、模型名单与模板多选见 `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`、`web/src/features/channels/lib/channel-form.ts`，共享模板库见 `web/src/features/system-settings/general/override-templates/`；测试见 `controller/request_key_rename_test.go`、`model/log_filter_test.go`、`web/src/features/channels/lib/__tests__/channel-custom-settings.test.ts`。（该功能随远端仓库合并进入本分支，`13a34a406`/`388f93f5c`。）
- **代理池新增「单位时间 Token 吞吐」观测维度（Proxy Prompt-Token Throughput）**：在原有 TTFT（首字时延）之外，新按 (渠道 × 模型 × 代理别名) 额外记录 `prompt_tokens / TTFT`（tokens/s），作为大 prompt 场景下代理传输速度的粗略信号；仅在拿到正的 prompt token 数时才记录这一项，不影响原 TTFT 采样本身。渠道统计弹窗「代理调度」表与「系统设置 → 模型 → 代理池」列表均新增「Prompt tokens/s」展示列；新增 Prometheus 直方图 `newapi_proxy_prompt_tokens_per_ttft_second{channel,proxy,model}`。该维度当前只做**展示**，不参与分流闸门或权重计算。实现见 `service/proxy_ttft.go`（`proxyTtftStat.tokenSpeeds`/`RecordProxyTTFT`/`ProxyTtftAliasSummary`/`ProxyAliasRoutingStat`）、`service/proxy_health.go`、`pkg/prom_metrics/metrics.go`，`service/proxypool/` 下的镜像实现同步更新；`service/quota.go`、`service/text_quota.go` 在结算处多传入本次请求的 `prompt_tokens`。前端 `web/src/features/channels/components/dialogs/channel-stats-dialog.tsx`、`web/src/features/channels/types.ts`、`web/src/features/system-settings/models/proxy-pool-section.tsx`、`web/src/features/system-settings/types.ts`。
- **Claude 缓存优化新增「稳定基础前缀断点」「无标记兜底断点」「保留上一轮断点」三项独立全局开关**：此前这三项行为分别硬编码耦合在「修正错位的缓存断点」（`cache_breakpoint_fix_setting`，决定是否同时启用稳定基础前缀断点）与是否开启请求骨架诊断（`model.CacheSkeletonEnabled()`，决定是否保留上一轮断点）上，客户想单独开启或关闭某一项做不到。现拆成三个独立的「系统设置 → 运维 → 系统行为」分组×模型规则——`stable_base_breakpoint_setting`（稳定基础前缀断点）、`fallback_cache_breakpoint_setting`（无标记兜底断点）、`retain_previous_breakpoint_setting`（保留上一轮断点，不再与诊断日志开关绑定），均默认不启用，互相独立，和原有 `cache_breakpoint_fix_setting`（修正错位断点）也相互独立。客户级覆盖（`model.CustomerClaudeCacheOptimization`）相应补上这三项缺失的独立开关。单个客户显式配置时，未配置某项规则默认不继承全局同名规则（与此前客户覆盖整体生效、仅自动继承缺省规则的既有语义一致）。实现见 `setting/operation_setting/group_model_rule.go`（新增 `ShouldUseStableBaseBreakpoint`/`ShouldUseFallbackCacheBreakpoint`/`ShouldRetainPreviousBreakpoint`）、`relay/claude_handler.go`（`resolveClaudeCacheOptimization`），前端 `web/src/features/system-settings/general/system-behavior-section.tsx`；回归用例见 `relay/claude_handler_cache_ttl_test.go`。
- **Claude 易变尾部标记可配置（Claude Volatile Tail Patterns）**：此前判断「哪些末尾消息文本会每轮变化、不该打缓存断点」写死为单一字符串 `<total_tokens>`。现新增「系统设置 → 运维 → 系统行为」配置项 `claude_volatile_tail_pattern_setting.patterns`（字符串数组，留空回退内置默认值），供「修正错位的缓存断点」与「无标记兜底断点」两条路径共用判断逻辑；此前「无标记兜底断点」路径本身并未检查末尾块是否为易变标记，存在把断点打在每轮都变的提醒文本上、断点写了也读不到的缺口，本次一并修复。实现见 `setting/operation_setting/group_model_rule.go`（`ClaudeVolatileTailPatternSetting`/`GetClaudeVolatileTailPatterns`）、`relay/claude_cache_breakpoint.go`（`claudeTextHasVolatilePattern`），前端 `web/src/features/system-settings/general/claude-volatile-tail-patterns-field.tsx`；回归用例见 `setting/operation_setting/group_model_rule_test.go`、`relay/claude_handler_cache_ttl_test.go`。
- **Claude `clear_at` 断点合法性判定补充 `never` 值**：此前判断一条消息是否因 `clear_at` 被限定为「仅本轮有效、不能打断点」时，只要 `clear_at` 字段存在就一律跳过；但协议里 `clear_at: "never"` 语义等同于未设置（永不清除），本应允许打断点。抽出共用判定 `claudeClearAtCacheable`（不存在或为 `"never"` 均视为可缓存），统一替换到无标记兜底断点与保留上一轮断点两处判断。实现见 `relay/claude_cache_breakpoint.go`。
- **Claude 缓存优化配置的「客户亲和性覆盖面」自检（Cache/Affinity Coverage Self-Check）**：Claude 的 prompt 缓存命中依赖连续轮次落到同一上游渠道/Key；若只开了缓存优化规则、却没有配套的渠道亲和性（`channel_affinity_setting`）规则覆盖 Claude 模型，缓存命中率实际上会趋近于零。现在任意一项全局 Claude 缓存优化规则（强制 1h TTL、修正错位断点、稳定基础前缀断点、无标记兜底断点、保留上一轮断点）与 `channel_affinity_setting` 任意一方变更时，或是进程启动读取尚未落库的选项默认值时，都会跑一次自检：若缓存优化规则非空但亲和性未启用或没有规则能匹配 `claude-sonnet-4-5` 这一代表性模型名，则写一条 `SysLog` 警告。该检查是启发式的、针对单个代表模型名探测，客户级覆盖或更细粒度的亲和性规则不在其可见范围内，可能有漏报或误报。实现见 `model/option.go`、`setting/operation_setting/channel_affinity_setting.go`（`CheckClaudeCacheAffinityCoverage`）；回归用例见 `setting/operation_setting/channel_affinity_setting_test.go`。
- **上游请求 ID 回传给客户端响应头**：此前网关只把上游请求 ID（默认读 `X-Oneapi-Request-Id`，或按渠道配置的 `UpstreamRequestIdHeader` 读取）记录进 Gin 上下文供内部日志使用，不会出现在返回给客户端的响应头里。现在解析到该 ID 后同时写入响应头 `X-Upstream-Request-Id`，便于客户端侧用上游请求 ID 排障；同时 `ShouldCopyUpstreamHeader` 新增对 `X-Upstream-Request-Id` 本身的过滤，防止上游若也返回同名头被直接透传、与网关自己写入的值冲突。实现见 `relay/channel/api_request.go`、`service/http.go`；回归用例见 `relay/channel/api_request_redirect_test.go`、`service/http_test.go`。
- **通用日志新增「会话ID来源」筛选（Session ID Source Filter）**：管理员可在通用日志筛选栏按会话 ID 的推断来源过滤，覆盖 relay 实际写入 `other.session_id_source` 的全部 5 种取值——`metadata.user_id`（Claude 显式字段）、`content-fallback`（Claude 内容回退推断）、`prompt_cache_key`（客户端自带）、`gateway.prompt_cache_key`（网关按会话自动生成）、`client_metadata.session_id`（OpenAI Responses 协议）。筛选为多选 + 包含/排除语义，匹配未建专用索引列的 `other` JSON 文本，需配合时间范围使用。后端见 `model/log.go`（`LogFilter.SessionIdSource`、`applyLogOtherFieldFilter`）、`controller/log.go`；前端见 `web/src/features/usage-logs/components/common-logs-filter-bar.tsx`、`constants.ts`（`SESSION_ID_SOURCE_OPTIONS`）；回归用例见 `model/log_filter_test.go`、`web/src/features/usage-logs/lib/__tests__/admin-filter-params.test.ts`。

### 优化 Changed
- **Claude 缓存断点调整步骤改为「失败即跳过，不中断请求」**：`ClaudeHelper` 中无标记兜底断点、修正错位断点、稳定基础前缀断点、保留上一轮断点四个调整步骤，此前任一步骤出错都会直接以 `ErrorCodeConvertRequestFailed` 中断整个请求；但这些步骤本身都是在协议已校验通过的请求体上做的锦上添花优化，不应该因为优化逻辑出错而拒绝一个本来合法的请求。现改为单步失败只记录错误日志（`logClaudeCacheStepError`）、跳过该步骤，请求继续按未调整前的内容往下走。实现见 `relay/claude_handler.go`。
- **缓存断点调整跳过时补充「透传模式」标记**：客户端若配置了请求体透传（pass-through），请求骨架记录时会跳过全部缓存优化步骤，此前骨架的 `fixed` 字段对此和「本来就没什么可调整」的情况一样显示为空，难以区分「配置了优化规则但因透传被跳过」与「没配置优化规则」。现仅当该客户/分组确有至少一项缓存优化规则生效时才标记 `fixed=pass_through`，管理员日志详情弹窗按 `added`/`moved`/`pass_through`（「跳过：透传模式」）分别展示文案。实现见 `relay/claude_handler.go`（`claudeCacheOptimizationConfigured`）、`web/src/features/usage-logs/components/dialogs/cache-skeleton-dialog.tsx`；回归用例见 `relay/claude_handler_cache_ttl_test.go`。
- **Claude 缓存断点保留状态改为可选 Redis 持久化**：此前「保留上一轮断点」的会话状态只存在进程内存（容量上限 4096、按最早过期淘汰），多实例部署下断点保留只对落在同一实例的请求生效。现改用新的 `pkg/cachex.HybridCache` 封装：Redis 已启用时写入 Redis（命名空间 `new-api:claude_cache_breakpoint_state:v1`，按 TTL 原生过期），否则回退为原来的进程内 LRU 缓存（基于 `github.com/samber/hot`）。写入仍做「更新时间更早的快照不得覆盖更新的」校验，但多实例下这一读-改-写对 Redis 并发写入不是原子的，竞态的后果仅是旧快照多保留一小会儿，不会把更旧的状态当新状态提供。注意：**只有 Redis 已启用（`common.RedisEnabled` 且 `common.RDB` 非空）时才会用 Redis**，否则仍是单实例本地可见。实现见 `relay/claude_cache_breakpoint_state.go`；回归用例见 `relay/claude_cache_breakpoint_state_redis_test.go`（基于 miniredis）、`relay/claude_handler_cache_ttl_test.go`。
- **字节缓存覆盖率 / 缓存损失量统计补充口径免责声明**：「缓存优化效果对比」与「Claude 缓存损失量统计」两处汇总新增提示文案，说明这两套数值是基于字节的近似估算，不是精确的 token 对账，不同语言与内容类型的字节/token 比例存在差异，不应作为计费对账依据。实现见 `web/src/features/usage-logs/components/cache-coverage-summary.tsx`、`web/src/features/usage-logs/components/cache-loss-breakdown.tsx`。

## [2026-11-03]

### 新增 Added
- **缓存优化效果对比**：对相同会话内容分别回放原始策略与实际优化策略，在会话诊断、会话筛选汇总和客户统计中展示估算字节覆盖率、带符号的百分点/字节净变化及可比较轮次。两侧独立维护缓存历史，统一文本表示并排除缓存标记字节，汇总按总字节加权；缺少优化前骨架的旧记录显示不可用，真实上游 token 命中率单独展示。
- **Claude 缓存损失量统计**：单会话诊断和会话筛选汇总增加未缓存 token 的互斥分类，展示首次请求、输入变化、断点位置、TTL、回查窗口及并发等原因的估算损失量；统计仅覆盖成功且可分析的 Claude 请求，不将全部未缓存量视为可挽回损失。
- **客户级 Claude 缓存优化**：客户编辑抽屉增加强制 1h TTL、易变尾部断点修复、保留上一轮成功写入位置、稳定基础前缀断点和无缓存标记兜底五项独立开关；每项支持模型前缀多选，开启且未选前缀时对全部适用模型生效。未配置客户继承原行为，显式配置覆盖旧规则，无标记兜底默认关闭。
- **成功缓存锚点保留**：以成功响应的缓存写入信息记录短期进程内锚点，后续请求在完整前缀、角色、消息边界和参数一致且未过期时恢复断点，减少断点推进过快造成的回查失效；状态按客户、令牌、会话、渠道及模型等维度隔离并限制容量。
- **基础前缀与无标记兜底**：可为稳定 system/tools 前缀增加断点；对完全没有缓存标记的请求，可在最后一个合法内容块补充 5m 断点，并与强制 1h TTL 联动。保留客户端已有缓存标记，跳过 thinking、空文本及临时消息，请求体透传路径不执行改写。
- **客户统计入口**：客户行增加统计图标，支持按日期查看该客户的调用统计与缓存损失分析；缓存分析按匹配会话完整回放，并提示共享会话及时间范围边界。

### 优化 Changed
- **日志筛选栏布局**：使用自适应栅格替代固定 12 列布局，分组、节点名称和客户 ID 使用较宽输入区域，减少筛选项挤压。
- **会话历史回填入口**：会话页隐藏历史回填按钮，并停止查询该入口的后台状态。
- **ClickHouse 日志查询效率**：错误日志的 HTTP 状态码落地为真实索引列（`status_code`），筛选时优先走该列精确匹配，仅历史行回退到 Other JSON 的 LIKE 扫描；`request_id`/`upstream_request_id`/`downstream_request_id`/`customer_id`/`error_signature_hash` 补充 `bloom_filter` 跳数索引，减少宽时间范围精确查询的逐 part 扫描。详见 `docs/log-query-optimization.md`。
- **明细错误页模型列**：模型徽章增加悬浮提示展示完整「模型名 × 次数」，最大宽度从 180px 调整到 260px（与客户列一致），避免长模型名被截断且无法查看完整内容。

### 修复 Fixed
- **资源类型错误处理**：修正资源类型选择器的错误处理函数导入，恢复创建、删除等失败场景的统一错误提示。
- **会话页面滚动**：会话列表随页面自然展开，容器支持整体纵向滚动，避免缓存统计区域挤压下方日志表格。
- **缓存断点边界**：修正 20 块回查窗口边界，补充顶层自动缓存的槽位计算与 TTL 覆盖，并防止较早请求晚完成时覆盖较新的成功锚点。
- **通用日志错误关键字筛选失效**：`LogFilter.apply` 此前要求「类型=错误」才应用错误关键字过滤，但前端「错误关键字」输入框与「类型」下拉框相互独立，填关键字时不会自动把类型切到「错误」，导致类型留在「全部」或其它值时关键字条件被静默忽略。改为只要填写了关键字即按日志内容匹配，不再要求类型必须为错误。
- **代理池别名改回「普通类型」不重启不生效**：`ConfigManager.updateConfigFromMap` 对 `[]struct`/`struct` 字段改配置时直接在已有值上 `json.Unmarshal`，新 JSON 里缺失的字段（如 `omitempty` 的 `proxy_type`）不会被清空、沿用旧值，行为与此前已修过的 `map` 字段不一致。代理池别名先存为 `tp`/`force_http` 再改回「普通类型」保存后，已加载过该别名的进程（含周期 `SyncOptions` 重载）内存里 `ProxyType` 仍残留旧值，继续按旧类型把请求的 `https` 目标强制降级为 `http` 转发给正向代理，导致代理端或上游返回 `308`，且仅重启进程才会恢复正常；数据库本身的值是正确的。现 `Slice`/`Struct` 分支改为和 `Map` 分支一样先建全新零值再反序列化整体替换，热更新即时生效、无需重启。实现见 `setting/config/config.go`（`updateConfigFromMap`），回归用例 `setting/config/config_test.go`（`TestUpdateConfigFromMap_SliceOmitemptyFieldCleared`）。

## [2026-11-02]

### 新增 Added
- **渠道资源类型**：管理员可维护资源类型，并在创建、编辑渠道时关联类型；渠道列表展示类型，删除仍被渠道引用的类型会被拒绝。
- **会话缓存汇总诊断**：使用日志会话页根据当前筛选条件展示缓存命中率及策略模拟结果；新增客户信息展示，普通日志显示客户标识，会话展示涉及的客户及名称。

### 优化 Changed
- **管理访问令牌权限**：为会话缓存汇总分析接口补充 `log:read` 作用域规则，并将资源类型管理接口纳入仅管理员认证路由。
- **系统行为安全限制**：演示站点模式和自用模式不能从关闭状态开启，但可关闭已开启项；关闭“允许未配置价格的模型”仍需二次确认。
- **代理池解耦准备**：整理代理健康状态、TTFT 与连接耗时分流相关实现及测试到独立服务目录，当前尚未接入运行调用链。

- **会话缓存诊断：展开轮次补「渠道 / Key」列，汇总行拆分上游损失**：「展开轮次」表在「实际 / 预期」命中率两列之后新增「渠道 / Key」列（展示该轮实际落到的 `channel_id / key_idx`），用于判断某轮缓存丢失是否伴随渠道或密钥切换。缓存诊断汇总行原本只给一个「上游损失」合计，现按归因拆成**本层渠道切换**与**上游未命中**两项分别展示：前者汇总归因为「渠道或密钥切换」的轮次未读到的预期命中 token（新 api 这层换了渠道/Key 导致丢缓存），后者汇总归因为「上游未命中」的轮次（本层渠道/Key 稳定、仍由上游内部切换等原因丢命中），两者均按 `max(0, 预期命中 − 实际命中)` 累加；汇总行其余两段（断点位置不当、TTL 过期）口径不变。实现见 `web/src/features/usage-logs/lib/cache-skeleton.ts`（`skeletonUpstreamSplit`）、`web/src/features/usage-logs/components/session-cache-diagnosis.tsx`，回归用例 `web/src/features/usage-logs/lib/__tests__/cache-skeleton.test.ts`。
- **TTFT 分流叠加连接耗时维度 + 代理延迟 Prometheus 指标**：在 2026-11-01「代理按首字时延（TTFT）动态分流」基础上增加**连接耗时（dial+TLS 建连耗时）**作为第二个分流信号。原代理池连接耗时只按别名全局采样、不分渠道；现按 **(渠道 × 代理)** 记录（建连耗时模型无关）并接入分流：①与 TTFT **叠加**——头部 TTFT 连续慢**或**头部**正向**代理连接耗时连续 ≥ 阈值（`proxy_setting.ttft_routing.connect_threshold_ms`，默认 1000ms）任一满足即开闸分流，权重按两者**乘积** `1/平均TTFT × 1/连接P90`，两者都恢复才关闸；②**反向代理（tp）固定「第一档」**——其连接耗时只量到网关→代理内网、与正代隧道不可比，故不参与连接加权（权重取最快正向代理的 P90、不被自身连接耗时降权），但 TTFT 照常参与、实测连接百分位照常展示；③只有**正向代理之间**按连接 P90 加权。渠道统计弹窗「代理调度」表在「平均首字延迟」后新增 **连接 P50 / P90(\*) / P99** 三列（\* 标记 P90 为参与调度的档位），反向代理行标注 `(反代)`。另把代理延迟导出为 **Prometheus histogram**（与内存里选路用的环形/闸门统计相互独立、互不影响）：新增 `newapi_proxy_connect_seconds{channel,proxy}`（建连耗时，模型无关），TTFT 侧 `newapi_proxy_ttft_seconds{channel,proxy,model}` 此前已有。连接状态同样为**进程内、按节点各算、重启清零**；仅成功建连（新连接、非复用）产生连接样本。实现见 `service/proxy_ttft.go`（`proxyConnectState`/`buildProxyConnectSnapshot`/合并权重与闸门）、`service/proxy_health.go`（`RecordProxyConnectResult` 成功分支记录）、`setting/system_setting/proxy_setting.go`（`ConnectThresholdMs`）、`pkg/prom_metrics/metrics.go`（`newapi_proxy_connect_seconds`），前端 `web/src/features/channels/components/dialogs/channel-stats-dialog.tsx`、`web/src/features/channels/types.ts`。

### 修复 Fixed
- **明细错误统计读不到签名导致页面为空（LogOther 重构兼容）**：错误日志写入改走强类型 `LogOther` 后，`error_signature` / `error_signature_hash` / `customer_id` 被收进 `other.admin_info`（经 `SetAdmin` 写入），而明细错误统计的读取路径仍只认 `other` 顶层的扁平字段，导致每条错误行都被判为「无签名」而跳过，明细错误页在仍有错误日志写入的情况下显示为空。现 `errorSignatureOther` 优先从 `admin_info` 段读取这三个字段，读不到再回退旧版顶层写法（LogOther 重构之前的历史日志），签名哈希、签名原文与客户归因均按此优先级解析。`GetErrorSignatureStats` 与 `GetErrorStatsGrouped` 两条聚合路径一并修正。实现见 `model/error_stats.go`，回归用例 `model/error_stats_test.go`（`TestErrorSignatureOtherReadsLogOtherAdminInfo` 锁 LogOther 写入格式、`TestErrorSignatureOtherLegacyTopLevel` 锁旧版顶层兼容）。

## [2026-11-01]

### 新增 Added
- **模型定价编辑页「从 models.dev 填充」（Fill Pricing from models.dev）**：模型编辑抽屉「定价」区在「恢复默认定价」旁新增按钮，点击后由后端从 `https://models.dev/api.json` 生成一条计费表达式（`tiered_expr`，USD / 1M tokens）填入编辑器**草稿**，不写库；管理员可修改后自行点保存，保存仍走现有校验与 `expected_version` 冲突检查，填充本身不会让模型变得不可访问。模型匹配：先精确匹配，再忽略 Bedrock 区域/`anthropic.` 前缀与 `-v1:0` 后缀、把 Vertex `@日期` 视为 `-日期`，最后去掉日期后缀再比。来源优先级：官方（anthropic / openai）→ 云厂商（amazon-bedrock / google-vertex / azure）→ 其余转售商中价格字段最全者。长上下文按 `tiers` 生成 `len <= 阈值 ? … : …` 分支（`272001` 这类写法归一为 `272000`；不使用名称有误导的 `context_over_200k`）。models.dev 缺失的价格按厂商规则推算并在提示中列出：Claude 缓存读 0.1×、5 分钟写 1.25×、1 小时写 2× 输入价；GPT 有 30 分钟写入价时补 12 小时写 = 2×。表达式返回前先编译校验。接口 `GET /api/ratio_sync/models_dev_suggestion?model=`（RootAuth，访问令牌作用域 `option:read`），models.dev 数据在后端缓存 10 分钟。注意：部署环境需放开到 `models.dev` 的出站访问；models.dev 是社区聚合数据，可能滞后或有误（如部分转售商条目缺缓存价），填充后请人工核对。实现见 `controller/models_dev_pricing.go`、`web/src/features/model-pricing/model-pricing-panel.tsx`、`web/src/features/system-settings/models/model-pricing-sheet.tsx`（`applyBillingExpr`）。
- **修正错位的 Claude 缓存断点（Cache Breakpoint Fix）**：「系统设置 → 运维 → 系统行为」新增「修正错位的缓存断点」规则（`cache_breakpoint_fix_setting.rules`，按 分组×模型 命中，**默认不启用**）。命中的 Claude 请求若把最后一个 `cache_control` 断点打在易变的末尾消息上（末尾的 mid-conversation system 消息，或含 `<total_tokens>` 提醒的用户消息），网关在它前面最后一个可缓存的稳定块上补一个断点（沿用原断点的类型与 TTL；thinking 块和空文本跳过；目标是纯字符串内容时改写成等价的单个 text 块），让下一轮能读回缓存前缀。断点还有空位（上限 4 个）时只新增、不动原断点；4 个已用满且末尾是 `<total_tokens>` 提醒，或末尾是带 `clear_at` 的本轮作用 system 消息时，改为把原断点移过去；末尾 system 消息不含已知易变标记时只新增、不删除。调整在强制 1h TTL 之前执行，新断点同样会被提升为 1h。调整结果（`added` / `moved`）写入请求骨架的 `fixed` 字段。实现见 `relay/claude_cache_breakpoint.go`、`relay/claude_handler.go`。注意：只识别 `<total_tokens>` 这一种易变标记；尚未用真实流量验证命中率改善。
- **OpenAI 请求自动补 `prompt_cache_key`（Prompt Cache Key Injection）**：同页新增「注入 prompt_cache_key」规则（`prompt_cache_key_injection_setting.rules`，按 分组×模型 命中，`auto` 分组按实际落到的分组判断，**默认不启用**）。命中的 Chat Completions / Responses 请求如果没带 `prompt_cache_key`，网关按「模型 + API Key + 对话锚点」生成一个稳定的 key（`gw-` 前缀 + 16 位哈希），同一对话的每一轮都相同，使其路由到持有缓存前缀的上游机器。对话锚点：Chat 取前两条消息（通常是 system 与首个 user）；Responses 优先取客户端 `client_metadata.session_id`（Codex 会带），否则取 instructions + 第一个 input 项。客户端自带的 key 不覆盖；生成的 key 同时作为会话 ID 记录，来源标为 `gateway.prompt_cache_key`。实现见 `relay/openai_prompt_cache_key.go`、`relay/compatible_handler.go`、`relay/responses_handler.go`。
- **使用日志新增「节点名称」筛选（Node Name Filter）**：集群多节点部署时，可按处理请求的节点（`NODE_NAME`，未配置时为主机名）筛选使用日志。新增 `logs.node_name` 真列（`varchar(128)` + 索引 `idx_logs_node_name`，`default:''`），`RecordConsumeLog` / `RecordErrorLog` / `RecordRetryOverrideLog` 写入当前节点，`RecordTaskBillingLog` 写入任务发起节点（为空回退当前节点）；`other.admin_info.node_name` 照旧双写。本机 ClickHouse 建表 DDL 带该列，老表启动时 `ADD COLUMN IF NOT EXISTS node_name` 补列，MySQL/SQLite 走 `AutoMigrate`。筛选与模型/分组同语义：多选 + 包含/排除（`node_name` / `node_name_op=exclude`），仅管理员生效，非管理员返回时清空该列；列表、统计、导出与统计分析浮窗均生效。新增 `GET /api/log/nodes`（AdminAuth）返回节点心跳表 `system_instances` 中的节点名作为下拉候选，也可手填已下线的节点。注意：**加列之前的历史日志该列为空**，按节点筛不到；会话视图（`log_sessions`）未加节点维度；`logshipper.Row`（本地文件 → 集群 ClickHouse）未加该列，集群侧仍需从 `other.admin_info.node_name` 读取。实现见 `model/log.go`、`controller/system_info.go`、`web/src/features/usage-logs/components/common-logs-filter-bar.tsx`。
- **请求骨架扩展到 OpenAI Chat Completions 与 Responses**：此前骨架只记录原生 Claude Messages。现 Chat Completions（`relay/compatible_handler.go`）、Responses（`relay/responses_handler.go`）与 Chat→Responses 转换路径（`relay/chat_completions_via_responses.go`，记录实际发往上游的 Responses 请求体）也在每次尝试结束时写骨架：按 tools → 结构化输出 schema → instructions → 消息/输入项的顺序记录块哈希，`param_fp` 为模型 + tool_choice + reasoning effort，另记 `prompt_cache_key` 哈希、`prompt_cache_retention` 与 `previous_response_id`。`cache_skeletons` 表新增 `provider`（默认 `claude`，老数据自动归为 Claude）、`cache_key_hash`、`retention` 三列，老表启动时 `ADD COLUMN IF NOT EXISTS` 补列。会话缓存诊断按 provider 选择缓存模型：OpenAI 按自动前缀缓存模拟（≥1024 token 才缓存、命中按 128 token 取整、内存缓存按 10 分钟过期、`24h` 保留为一天），三档上限为「前缀匹配上限 / 统一 prompt_cache_key 后 / 统一 key + 24h 保留」，新增归因「prompt_cache_key 变化」「使用 previous_response_id」（此时前缀在上游，按实际计）。日志详情的「请求骨架 · 查看」入口改为对所有消耗/错误日志开放，无骨架时由弹窗提示。实现见 `relay/cache_skeleton.go`、`model/cache_skeleton.go`、`model/cache_skeleton_analysis.go`。
- **Claude 请求骨架记录与查看（Cache Skeleton）**：用于回应客户对 prompt 缓存命中率的质疑。每次原生 Claude Messages 尝试（成功或失败、含重试）结束时记录一份「请求骨架」：按缓存前缀顺序 tools → system → messages 列出每个块的 xxHash64（带 `cache_control` 的块先去掉该字段再哈希）、块类型、名称（工具名 / tool_use 名）、字节数、每条消息的起始块、断点位置与 TTL，外加参数指纹 `param_fp`（模型 + tool_choice + thinking + effort）、`lane`（模型 + tools + 首个 system 块，区分主会话与 subagent）、渠道/key、会话 ID 与本次 usage。**不保存任何正文或预览**。骨架只写入 ClickHouse 日志库的新表 `cache_skeletons`（`MigrateCacheSkeletons` 挂在 `migrateClickHouseLogDB`，按天分区，`CACHE_SKELETON_TTL_DAYS` 默认 14 天整分区过期，可选 `CACHE_SKELETON_STORAGE_POLICY`）；日志库不是 ClickHouse 时功能自动关闭，也可用 `CACHE_SKELETON_ENABLED=false` 关闭。写入走有界队列（4096）+ 后台批量（每 500 行或 2 秒），队列满时丢弃，不阻塞转发。接口 `GET /api/log/cache-skeleton?request_id=`（AdminAuth）；管理员日志详情（Claude 的消耗/错误日志）新增「请求骨架 · 查看」，弹窗按 tools / system / 每条消息分组展示块哈希前 8 位、大小、累计字节与断点，断点打在末尾 mid-conversation system 消息（如 `<total_tokens>` 提醒）上时标 ⚠。实现见 `model/cache_skeleton.go`、`relay/cache_skeleton.go`、`relay/claude_handler.go`。
- **会话缓存诊断（Session Cache Diagnosis）**：使用日志「会话」视图展开某个会话时，管理员可在顶部看到缓存诊断。后端按 lane 回放该会话的骨架做缓存模拟（前缀哈希为键；写入请求结束后才可读，按 TTL 过期、读取续期；断点回看 20 块；低于模型缓存下限的断点不写缓存；token 按字节占比由本轮真实输入总量分摊估算），给出四档命中率——**实际 / 客户端断点上限 / 网关断点调整后 / 调整 + 1h TTL**，以及上游损失、断点位置不当、TTL 过期三段损失 token。「展开轮次」后逐轮显示时间、间隔、前缀条（灰=与上轮相同、红=变化、绿=新增、竖线=断点）、实际/预期命中率与归因（首轮、低于缓存下限、无断点、参数变化、工具或系统提示变化、与上一轮并发、TTL 过期、断点位置不当、渠道或密钥切换、上游未命中、符合预期），并可直接打开该轮骨架。接口 `GET /api/log/cache-skeleton/session?session_id=`（AdminAuth，单会话最多分析 2000 次尝试）。实现见 `model/cache_skeleton_analysis.go`、`web/src/features/usage-logs/components/session-cache-diagnosis.tsx`。注意：**只有升级后进来的请求才有骨架**，此前的日志与会话不显示；「调整后」两档为模拟值（网关断点调整见上方「修正错位的 Claude 缓存断点」，按规则开启）；模拟结果目前只用单测数据验证过。
- **会话历史回填（临时入口）**：root 在「会话」视图工具栏可点「回填历史会话」，填天数（1–366，默认 7）后把会话表建立之前的日志导入会话表；按本地自然日从近到远逐天执行（每天一条 `INSERT … SELECT`，`max_threads=2`、内存上限 4GB、两天之间停 2 秒），只导入物化视图创建时刻之前的日志，已完成的日期记在 `log_sessions_backfill`，重复触发自动跳过、不重复计数。接口 `POST /api/log/sessions/backfill?days=N` / `GET /api/log/sessions/backfill`（RootAuth，访问令牌作用域 `ops:write` / `ops:read`）。回填状态只保存在执行它的节点内存里，经负载均衡访问时可能看到别的节点的空闲状态；两个节点同时各触发一次无法防止重复。回填会扫描日志表，**请在低峰期执行**。实现见 `model/log_session.go`、`web/src/features/usage-logs/components/sessions-backfill-button.tsx`。
- **数据看板新增「渠道统计」分区（Channel Statistics）**：仿照「用户统计 / 客户统计」，在数据看板新增「渠道统计」分区（仅管理员可见，路由 `/dashboard/channels`）。按渠道聚合预聚合表 `quota_data`（`channel_id <> 0`），提供渠道消耗排行与趋势两张图，支持时间范围 / 粒度 / Top N / 金额口径切换。金额口径三选一：**渠道折后金额**（`channel_stat_quota`）、**客户折后金额**（`customer_stat_quota`）、**利润**（客户折后 − 渠道折后）。渠道名解析复用缓存/批量查 `channels` 表（`model.ResolveChannelNames`，缺名回退 `#<id>`）。**无需 DB 迁移**：`quota_data` 已有 `channel_id` 与三项金额列。接口 `GET /api/data/channels`（AdminAuth）。实现见 `model/usedata.go`（`GetQuotaDataGroupByChannel`/`ResolveChannelNames`）、`controller/usedata.go`（`GetQuotaDatesByChannel`）、`web/src/features/dashboard/components/channels/channel-charts.tsx` 及 `lib/charts.ts` 的 `processChannelChartData`。
- **代理按首字时延（TTFT）动态分流（TTFT-aware Proxy Routing）**：渠道引用代理池多别名时，原逻辑是「首个健康代理挂了才切下一个」，应对不了「代理活着但变慢」。现按**渠道级开关**可选开启（渠道编辑抽屉「代理池别名」下新增开关，`proxy_ttft_routing_enabled`，默认关）：按 (渠道 × 模型 × 代理别名) 记录流式首字时延，当**头部代理**连续 N 次 TTFT ≥ 阈值时打开「分流闸门」，按概率把部分请求分到其他别名，并按各代理实测 TTFT 的倒数加权；尚无样本的备用代理按头部慢速估值以获得被探测机会；头部份额被钳制在 `[1−最大分流比例, 1−最小探测比例]`，保证头部与备用都持续有流量产生数据；头部恢复（连续 N 次低于阈值）后闸门关闭、回到严格优先级。连接层失败冷却始终优先于 TTFT（不健康代理永不被选）。全局阈值参数（慢阈值 / 连续次数 / 最小探测比例 / 最大分流比例 / 采样窗口，默认 `3000ms / 3 / 0.2 / 0.8 / 50`）在「系统设置 → 模型 → 代理池」新增表单（`proxy_setting.ttft_routing`），代理池每行新增「平均 TTFT」展示。另新增按代理维度的 Prometheus 直方图 `newapi_proxy_ttft_seconds{channel,proxy,model}`。注意：路由状态为**进程内、按节点各算各的**，重启清零；**仅流式且已出首字**的请求计入 TTFT；非流式/未出首字不参与。实现见 `service/proxy_ttft.go`、`service/proxy_health.go`、`relay/channel/api_request.go`、`service/quota.go`/`service/text_quota.go`、`pkg/prom_metrics/metrics.go`、`setting/system_setting/proxy_setting.go`、`relaykit/dto/channel_settings.go`，前端 `web/src/features/channels/lib/channel-form.ts`、`web/src/features/system-settings/models/proxy-pool-section.tsx`。
- **渠道列表新增「渠道统计」弹窗（Channel Statistics）**：渠道列表每行操作区新增统计按钮，打开只读的「渠道统计」弹窗（设计为可扩展容纳后续各类统计，当前首个分区为「代理调度」）。代理调度分区按 **渠道 × 模型炸开**，每个模型一张表展示各代理的**当前选择概率**（本节点实时按样本重算）、**累计实际命中次数**（本节点累计，统计所有真实选中、不限流式）、是否可用（冷却中标注）、TTFT 样本数与平均 TTFT，并以徽标标明该模型闸门状态（分流中 / 严格优先）；渠道未启用 TTFT 调度时概率按严格优先（头部 1、其余 0）展示；无流量时显示空态。接口 `GET /api/channel/:id/stats`（AdminAuth，权限 `authz.ChannelRead`），`data` 预留扩展后续统计分区。注意：概率与命中次数均为**本节点**口径、进程重启清零，与 Prometheus 维度一致。实现见 `controller/channel.go`（`GetChannelStats`）、`router/channel-router.go`、`service/proxy_ttft.go`（`ProxyTtftChannelStats`/`recordProxyPick`），前端 `web/src/features/channels/components/dialogs/channel-stats-dialog.tsx`、`data-table-row-actions.tsx`、`channels-dialogs.tsx`、`channels-provider.tsx`。

### 优化 Changed
- **使用日志高级筛选行布局调整**：把「客户名称 / 客户ID / 下游请求ID / 下游TraceTag」四个筛选合并到同一行（高级筛选最后一行），把「节点名称 / 是否重试 / 状态码 / 错误关键字」单独放一行，并加宽「节点名称」输入框，避免换行错位。实现见 `web/src/features/usage-logs/components/common-logs-filter-bar.tsx`、`logs-filter-toolbar.tsx`，布局回归用例 `web/src/features/usage-logs/components/__tests__/filter-rows.test.tsx`。
- **客户名 / 客户 ID 筛选兼容加列前的历史日志**：此前这两个筛选只按 `logs.customer_name` / `logs.customer_id` 真列精确匹配，加列之前的日志（值只在 `other.admin_info` 里）筛不到。现改为「真列精确相等 **或** `other` 中对应字段匹配」，历史日志也能筛出；`__none__`（其他 / 无客户信息）改为选出 `other` 中没有 `customer_id` 的日志。注意：带 `OR other LIKE` 后该条件**不再只走索引**，请配合时间范围使用。实现见 `model/log.go`。
- **Claude 兜底会话 ID 种子改为「API Key + 首条消息」**：客户端未带 `metadata` 会话信息时，网关生成会话 ID 的种子此前取带 `cache_control` 的 system/消息块文本；Claude Code 等客户端每轮都把断点挪到最新消息，导致同一对话被拆成很多单请求会话。现改为 API Key ID + 首条消息内容（去掉 `cache_control` 后），整段对话保持同一会话 ID，不同 Key 也不会落到同一会话。注意：**升级前后同一对话的兜底会话 ID 会变化**。实现见 `relay/claude_handler.go`。
- **会话视图改为 ClickHouse 独立表 `log_sessions`**：此前每次查询都在 `logs` 上按 `session_id` 实时 GROUP BY，且统计值只算查询时间窗口内的部分。现新增 AggregatingMergeTree 表 `log_sessions`（`ORDER BY (session_id, user_id)`），由物化视图 `log_sessions_mv` 在日志写入时自动汇总（只收 `session_id` 非空且非 `notfound` 的消耗/错误日志），网关写日志路径无改动；建表挂在 `migrateClickHouseLogDB`（master 节点），日志库不是 ClickHouse 时接口返回 `enabled=false`，前端显示「会话视图需要使用 ClickHouse 日志库」。**口径变化**：统计值改为整个会话的累计；时间筛选改为「会话的 [首次请求, 最近请求] 与所选区间有交集」；只有用户、令牌、模型（支持 `%` 模糊）、分组、渠道、客户、会话 ID 筛选生效，其余日志字段筛选在会话视图里不起作用（页面有提示）；请求间隔仍在读取当前页时从 `logs` 按会话计算，只统计该会话首次请求之后的消耗日志；展开会话显示其全部请求，不受时间范围限制。缓存命中率改由表内汇总（与统计分析浮窗同口径的 gross 命中率与命中/冷写入次数）。另给 `logs.session_id` 加 bloom_filter 跳数索引 `idx_logs_session_id`（新建表直接带、老表 `ADD INDEX IF NOT EXISTS`），只覆盖新写入的 part，老数据需低峰期手动执行 `ALTER TABLE logs MATERIALIZE INDEX idx_logs_session_id`。注意：**升级后会话表从零开始累计**，历史会话需手动回填（见上）；物化视图用 `IF NOT EXISTS` 创建，日后修改定义需手动删除重建；会话表不设过期删除。实现见 `model/log_session.go`、`model/main.go`、`controller/log.go`；ClickHouse 实测用例 `TestLogSessionsClickHouse`（需 `TEST_CLICKHOUSE_DSN`）。

### 修复 Fixed
- **使用日志「重试覆盖」类型筛选不生效**：日志页路由的 `type` 参数校验名单写死为 `0`–`7`，不含重试覆盖的 `8`；选「重试覆盖」后校验失败、回退成「全部类型」，筛出来的仍是普通日志。现校验名单改为从类型下拉选项 `LOG_TYPE_FILTERS` 生成，新增日志类型不会再漏。实现见 `web/src/features/usage-logs/lib/log-type-search.ts`。
- **Claude `clear_at` 消息被丢弃或被上游拒绝**：`ClaudeMessage` 新增 `clear_at` 字段原样透传（此前转换时被丢掉，本轮作用的提醒会变成永久消息）；请求中出现 `clear_at` 时自动在 `anthropic-beta` 头补上 `mid-conversation-system-clear-at-2026-08-21`；`clear_at=next_user_message` 的消息上的 `cache_control` 会被去掉（上游不允许本轮作用消息带断点）。实现见 `relaykit/dto/claude.go`、`relay/claude_cache_breakpoint.go`。

## v0.0.9（基于上游 QuantumNous/new-api v1.0.0-rc.47 re-baseline 开发）

> 本版本把既有自定义功能 re-baseline 迁移到上游 `v1.0.0-rc.47` 之上。下方仅记录本轮**真正新增或此前未记录**的变更；纯粹的端口恢复（从旧 `feature/custom-query-api` 分支迁移时丢失、现仅做还原，且已在下方历史条目中记录过的改动，如性能监控与过载保护解耦、成功率健康色阈值、客户名/ID 筛选下拉还原、少量硬编码英文补 i18n、零值金额行展示门控还原）不再重复列出。

### 新增 Added
- **使用日志新增「会话」分组视图（Usage Log Session View）**：Common 使用日志在「日志明细 / 统计分析」之外新增第三个视图「会话」（`web/src/features/usage-logs/index.tsx` 的 `view=sessions` Tab，路由 `$section.tsx` 的 `view` 枚举加 `sessions`）。该视图把**带真实会话 ID**（Claude metadata session / OpenAI `prompt_cache_key`，排除空值与 `notfound` 哨兵）的消耗/错误日志按 `session_id` 聚合成一行，按最近活动倒序；每行展示请求数、消耗/错误次数、首末时间、额度、prompt/completion tokens、涉及的用户/令牌/模型数量，以及**请求间隔（平均/最小/最大）**和与日志分析弹窗同口径的**缓存命中**单元格，行内可展开查看该会话的逐条请求（`usage-logs-sessions.tsx` / `session-requests-table.tsx` / `columns/session-columns.tsx`，另附 `__tests__/session-view.test.tsx`）。后端新增 `model.GetLogSessions`（`model/log_session.go`：按 `session_id` 分组聚合，`consume_count`/`error_count` 用 `CASE WHEN type=?` 条件计数，间隔取相邻消耗日志 `created_at` 之差、错误/重试不缩短间隔，缓存单元格复用 `classifyCacheRow`/`cacheCellFromAgg`；明细扫描上限 `logSessionScanLimit=200000`，命中则置 `Truncated`），接口 `GET /api/log/sessions`（`AdminAuth`，全量用户）与 `GET /api/log/self/sessions`（`UserAuth`，仅本人）在 `controller/log.go`（`GetLogSessions`/`GetUserLogSessions`）、`router/api-router.go` 与 `middleware/access_token_routes.go`（访问令牌作用域 `log:read` / `usage:read`）注册；前端经 `api.ts` 的 `getLogSessions` 与 `types.ts` 的 `LogSession`/`GetLogSessionsResponse` 消费。

### 优化 Changed
- **`customer_name` 提升为独立索引列并改走精确等值筛选（附非管理员隐私收敛）**：此前客户名只落在 `other.admin_info.customer_name`，筛选靠跨方言 `other LIKE` 子串匹配。现新增 `model.Log.CustomerName` 真列（`varchar(128)` + 索引 `idx_logs_customer_name`，`default:''`），`RecordConsumeLog`/`RecordErrorLog` 在解出下游 Trace-Tag 时把写时客户名快照**双写**进新列并保留 `other` 那份（随 `other` 继续同步远端 ClickHouse）；本机 CH 建表 DDL 与 `migrateClickHouseLogDB` 幂等 `ADD COLUMN IF NOT EXISTS customer_name` 补列（`model/main.go`），MySQL/SQLite 走 `AutoMigrate`。`LogFilter.CustomerName` 由 `other LIKE` 改为 `logs.customer_name = ?` **精确等值**（走索引，`model/log.go`）。**隐私修复**：`formatUserLogs`（非管理员返回路径）现额外清空 `CustomerName`/`CustomerId`/`DownstreamRequestId` 三个管理员专属下游列——此前 `customer_id`/`downstream_request_id` 会随非管理员列表泄漏，`customer_name` 为本轮新列一并收敛。注意：**筛选语义由子串匹配变为精确相等**；且**加列之前产生的历史日志**该列为空，仅 `other` 里有客户名，用列精确筛选查不到它们（新日志不受影响）。
- **索引精确标识符查询跳过非管理员时间范围限制**：此前非管理员查使用日志会被强制收敛到「最长可查时间窗」。现当筛选带**索引精确标识符**（`session_id` / `request_id` / `upstream_request_id`）时，这类查询无论时间范围都走索引、开销很低，故跳过该时间范围守卫——后端 `model.LogFilter.HasIndexedIdentifier()` 判定，`controller/log.go` 的 `GetUserLogs`/`GetUserLogSessions` 在非管理员且无索引标识符时才限制时间窗；前端 `lib/utils.ts` 的 `buildTimeRangeParams` 同步：设置了上述任一标识符时不再注入默认「今天」时间窗，避免精确查询被静默缩窄。
- **日志详情下游字段读取兼容旧版本写法**：旧版本把 `customer_id` / `customer_name` / `downstream_request_id` / `downstream_trace_tag` 直接写在 `other` 顶层（彼时尚无索引列、也未统一收进 `other.admin_info`）。详情弹窗（`details-dialog.tsx`）现对这四个管理员字段依次回退读取——先读顶层真列，再读 `other.admin_info.*`，最后回退旧版 `other.*` 顶层（`types.ts` 的 `LogOtherData` 补上这四个旧版字段类型），使**旧版本写入的历史日志**在新前端也能正确展示。

## [2026-10-29]

### 新增 Added
- **重试覆盖规则新增「终止并返回指定错误码」动作（`return_error`）**：此前 `phase4_retry_action` 只有「重试当前渠道」`retry_same_channel` 与「兜底下一渠道」`fallback_next_channel` 两种动作。现新增第三种 `return_error`——命中即**终止全部重试**，把失败响应重写为配置的 HTTP 状态码（及可选消息）返回给调用方。该动作**不钉渠道、不重置预算、不改写请求体**；走正常错误返回路径（预扣费在顶层照常退还、不计费）。审计日志额外记录 `return_status`/`return_message`，`status_code` 仍记原始上游状态码（触发原因）。实现见 `relay/retryrule/retryrule.go`（`ActionReturnError` + 缺省/越界状态码回退 429）、`controller/retry_fallback.go`（终止时重写 `apiErr` 并让主循环 `break`）。
- **`phase4_retry_action` 兼容字符串与对象两种写法**：可继续写字符串（如 `"fallback_next_channel"`），也可写对象 `{"action":"return_error","status_code":429,"status_msg":"..."}`。新增 `dto.RetryAction` 自定义 `UnmarshalJSON`（容错解析：坏值回落零值，避免因单条规则写错导致 `Channel.GetSetting` 丢弃整段渠道 setting）与 `MarshalJSON`（仅含 action 时回落为字符串形式，保持旧配置序列化不变）。

### 优化 Changed
- **重试覆盖动作裁决改为「首条命中生效」**：规则数组现按**优先级顺序**解释——一次失败响应只采用**首条** phase1&2 均命中的规则，由该规则的 phase3 改写与 phase4 动作**全权决定**，命中即停、不再看后续规则。此前的语义是「任一命中规则为 `fallback_next_channel` 则整体兜底，且 phase3 改写跨所有命中规则叠加」，会让规则顺序失效、并可能让宽泛的 fallback 规则静默压掉更具体的 retry 规则；新语义更可预测，顺序即优先级。`retryrule.CollectRewrites` 改为返回 `Decision{Matched,Rewrites,Action,ReturnStatus,ReturnMessage,MatchedIndex}`；`maybeApplyRetryRuleFallback` 改为返回 `(retry, terminate, detail)`，主循环遇 terminate 直接 `break`。审计日志 `rule_ids` 变为单元素（首条命中规则序号）。设计见 `docs/retry-override-design.md`。

## [2026-10-28]

### 优化 Changed
- **使用日志首屏统计开销优化（Stat 查询合并快路径）**：日志量大时，打开「使用日志」默认会对同一时间范围重复扫表——列表的 `COUNT(*)`、`SumUsedQuota` 的 `sum(quota)+count(*)`、`SumLogStatAmounts` 的三项金额 `SUM`，其中后两者 WHERE 完全一致却各扫一遍。新增 `model.SumLogStatWithAmounts`，用条件聚合在**一次扫描**内同时算出消耗 `quota`/请求数（`CASE WHEN type=consume`）与渠道折后/客户折后/利润三项金额；RPM/TPM 因是「最近 60 秒」独立窗口仍单独查（抽出共享的 `sumRecentRpmTpm`，`SumUsedQuota` 一并复用去重）。`controller.GetLogsStat` 仅在**无任何管理员专属筛选**（request_id/upstream_request_id/status_code/error_keyword/customer/trace_tag/error_signature/retry 全空）且类型为「全部或消耗」时走合并快路径（恰好命中默认打开场景），否则回退原两次查询。**行为不变**：`用量/RPM/TPM` 仍刻意不随 status_code/error_keyword 等筛选变化（见 `docs/log-filter-status-code.md`），合并仅发生在两者扫描行一致时；新增 `model/log_stat_combined_test.go` 断言「合并结果 == 原两次查询结果」并锁定具体数值。
- **使用日志统计徽标延迟加载**：顶部「请求数/用量/RPM/TPM/金额」徽标的统计请求改为首帧空闲后（`requestIdleCallback`，降级 `setTimeout`）再触发，让日志列表先渲染，统计随后补上（加载期间显示骨架），避免统计全表聚合与列表请求在打开瞬间争抢。
- **使用日志默认每页条数 100 → 30（桌面端）**：桌面端默认 `page_size` 由 100 降为 30（移动端保持 20），减少首屏行数与大 JSON 字段的传输/反序列化开销。

## [2026-10-27]

### 新增 Added
- **DEBUG 日志支持按「API Key（令牌名）+ 模型」维度开启（Scoped Debug Logging）**：全局 `DebugEnabled` 是全开/全关的总开关，开启期间所有流量都打明文、日志量巨大。新增按 `令牌名 + 客户端请求模型名` 维度的作用域开关，**默认全不开启**，用于「只排查某个客户的某个模型」。配置为 `debug_scope_setting.rules`（一组 `{token_name, model}` 规则，走 `config.GlobalConfig` 自动持久化与集群同步，零迁移）：`token_name` 是令牌名（非 `sk-` 原文），`model` 匹配**映射前的原始模型名**（`original_model`）；**两字段都必须命中且非空**（精确相等），空字段规则永不生效，要覆盖多个范围需写多条规则（规则间为或）。命中规则的请求即使全局关闭也会记录该请求的转发链路明文。实现上新增判定入口 `logger.DebugActive(ctx)`（`common.DebugEnabled || operation_setting.DebugScopeMatches(令牌名, 原始模型)`，从请求上下文读 `token_name`/`original_model`），`logger.LogDebug`/`LogJson` 及少数带 `if common.DebugEnabled` 前置守卫的转发链路日志点（`relay/compatible_handler.go` 透传原文 body、`relay/channel/openai/relay-openai.go`、`relay/helper/price.go`）改走 `DebugActive`，其余未加守卫的 `LogDebug` 自动继承作用域判定；系统级调试行（Redis、迁移日志、ollama、override 审计等）仍只跟随全局开关。UI 在「系统设置 → 操作设置 → System Behavior」分区 Debug 开关下方，可增删 `API Key + 模型` 规则行；新增文案补齐 en/zh（fr/ja/ru/vi 暂为英文占位，回落 en）。注意：**令牌名改名后旧规则会失配**（代价仅是本想开 DEBUG 没开成，补一条即可）；默认非透传模式下 body 日志仍是转换后的上游请求体，要原文需开渠道透传。设计见 `docs/debug-mode-runtime-toggle.md`。
- **系统信息页新增进程级指标（进程 CPU / 活跃请求 / RPM / 线程 / FD）**：集群「系统信息」实例表在原有机器级 CPU、内存、磁盘之外，新增 5 列进程级指标——进程 CPU（`gopsutil/process` 每 5 秒采样当前进程）、活跃 Relay 请求数、最近 60 秒完成的 Relay 请求数（RPM）、OS 线程数、文件描述符数。新增 `common/process_monitor.go`（`StartProcessMonitor` 后台 goroutine 采样，受 `monitor_enabled` 开关控制，采集失败时对应指标为 nil）与 `common/http_runtime_stats.go`（原子活跃计数 + 60 个按秒 bucket 的滑动窗统计 RPM）；`middleware/stats.go` 的活跃连接计数从本地 atomic 改为复用 `common` 共享统计。指标随 30 秒心跳写入 `system_instances.info`（JSON TEXT 字段，无需迁移），前端表格对缺失/不支持的指标显示 `-`（而非 0）。注意：**活跃请求数是进行中的 Relay 请求数，不是 TCP 连接数**；进程 CPU/线程/FD 依赖 gopsutil，平台或权限不支持时省略。新增表头文案已补齐 en/zh/fr/ja/ru/vi。
- **明细错误筛选区升级为「下拉选择 + 手动填入」并新增 4 个筛选框**：明细错误页筛选框全部改为可下拉可手填的 `Combobox`（`allowCustomValue` + `clearable`）——模型（`getEnabledModels`）、分组（`getGroups`）、渠道（从当前统计结果出现过的渠道派生选项，可手填任意 ID）；状态码、条数仍为纯数字输入。新增一行放「客户 / 客户ID」两个筛选框（沿用「使用日志」做法，拉取已配置客户列表作候选、含「其他（无客户信息）」哨兵 `__none__`、支持手填），以及「明细错误类型 / 错误归因」两个筛选框（候选取已配置别名与当前结果派生值）。客户筛选基于日志 `other.customer_id`/`customer_name`（`__none__` 选无客户行）；明细错误类型/错误归因是按签名从别名缓存**查询时解析**的值，不落库、无法走 SQL，故在后端 Go 侧对聚合结果过滤（`matchesErrorCategoryFilter`，对 signature / 明细错误类型 / 错误归因三种视图都生效，空值不过滤）。

### 优化 Changed
- **客户名/客户ID 筛选下拉组件通用化**：将「使用日志」里客户名/客户ID 的下拉+手填逻辑抽为共享组件 `features/customers/components/customer-filter-combobox.tsx`（内部拉取配置客户表、构建候选、含「其他（无客户信息）」哨兵），并在「使用日志」与「明细错误」两页复用，行为保持一致。
- **会话 ID 提升为 `logs.session_id` 真列，会话搜索改走列过滤**：此前会话 ID（Claude metadata session / OpenAI `prompt_cache_key`，无会话时为 `notfound` 哨兵）只落在消耗/错误日志 `other.session_id`，会话搜索靠跨方言 `other LIKE` 扫描。现新增 `model.Log.SessionID` 真列（`varchar(128)` + 索引，`default:''`），`RecordConsumeLog`/`RecordErrorLog` 从 `other` 取值写入新列并**保留 `other` 那份**（随 `other` 继续同步远端 ClickHouse，`logshipper.Row` 不动）；本机 CH 建表 DDL 与 `migrateClickHouseLogDB` 幂等 `ADD COLUMN IF NOT EXISTS` 补列，MySQL/SQLite 走 `AutoMigrate` 自动补列。`SearchLogsBySessionWindow` 由 `other LIKE` 改为 `session_id = ?` 精确列匹配（走索引，去掉 LIKE 转义逻辑及其测试）。前端日志详情「会话 ID」行做兼容：`UsageLog` 增加顶层 `session_id`，展示**先读列、列空回落 `other.session_id`**，来源 `session_id_source` 仍只读 `other`（未提列）。注意：**列加上之前产生的历史日志**仅在 `other` 里有会话 ID、列为空，会话搜索（时间窗查询）查不到它们；新日志不受影响。方案见 `docs/log-field-columnization.md`。

## [2026-10-26]

### 新增 Added
- **平滑停机中断请求日志**：复用现有消耗日志与错误日志记录进程因停机超时强制取消的请求，在管理员日志详情中展示实例、批次和中断时间；停机时停止登记新请求，超时后取消仍在处理的普通 HTTP/SSE 上游请求并等待有限时间完成日志收尾。覆盖范围和限制见 `docs/process-shutdown-logs.md`。
- **性能过载拒绝日志**：性能监控因 CPU、内存或磁盘超过阈值而在入口返回 503 时，按现有错误日志开关记录请求 ID、节点、资源使用率和阈值，并使用稳定错误签名参与错误统计。检查仍在鉴权和渠道选择之前，因此日志表示本节点拒绝、尚未调用上游，用户/令牌/渠道字段为空或为 0，不能用于归因上游渠道 503；开启错误日志时每次拒绝均尝试同步写入，过载期间会增加日志库写入压力。详见 `docs/system-overload-logs.md`。
- **使用日志统计模式前端原型**：在 Common 使用日志中新增「日志明细 / 统计分析」视图切换，统计模式复用现有筛选条件并默认使用所有类型（consume + error）。当前以假数据展示异步统计任务进度、按渠道指标对比、总体请求/成功率/重试率、最终状态分布、TTFT/TPOT/E2E 分布、四种缓存覆盖率和重复 consume 异常提示；渠道对比表支持按请求数、尝试次数、成功率、重试率、金额、TTFT P95、E2E P95、缓存命中 token 率和缓存命中请求率升降序排序。详细方案见 `docs/usage-log-statistics-design.md`。
- **客户折后金额标注系列折扣兜底**：消耗日志新增明确的折扣来源标记 `other.customer_discount_fallback`，命中系列时另记录 `customer_discount_matched_prefix`；未匹配系列折扣时，详情金额旁显示「未匹配系列折扣，兜底到 1」。配置的系列折扣恰好为 `1` 仍视为正常命中，绝不按数值判断兜底；历史日志缺少来源标记时不补判。
- **错误日志记录重试覆盖规则并支持筛选**：将触发重试覆盖的规则编号、描述、动作和改写摘要记录到对应错误日志的 `other.admin_info.retry_override`，列表与详情可查看；保留独立重试覆盖审计日志。新增 `retry_triggered` 真列与 `other` 双写，管理员高级筛选提供「触发重试：全部 / 是 / 否」，列表和统计共用筛选条件；是/否均限定错误日志，普通重试不算规则触发，历史未记录触发标记的错误归入「否」。规则信息按单次失败传递，避免串到后续重试；本机日志库自动补列，远端日志继续通过 `other` 同步。
- **上游首字时延 TTFT Prometheus 直方图（Relay TTFT Histogram）**：新增 `newapi_relay_ttft_seconds`（Histogram，标签 `{model,group,channel}`，桶 `0.1,0.25,0.5,1,2,5,10,20,30,60`），在结算/失败落点按 `RelayInfo.FirstResponseSeconds()` 观测——**仅流式且已出首字**才记（非流式/未出首字不计入）。三处落点（`service/quota.go`、`service/text_quota.go`、`controller/relay.go`）统一传入 TTFT。此前中继只有总时延直方图 `newapi_relay_duration_seconds` 与平均 TTFT，首字长尾无法出分位；现可用 `histogram_quantile` 出 TTFT 的 P50/P90/P99。`docs/prometheus-dashboard-promql.md` 补了指标表、桶说明与 TTFT 分位/按模型分解 PromQL。
- **使用日志流式列新增「纯生成速度」（扣首字）+ 精确总耗时 `other.lat`**：原 `t/s` 为 `completion/总耗时`（含首字等待），首字高时会被拖低、易误判为吐字慢。现在流式列在原总 t/s 旁**增**一个扣掉首字的纯生成速度 `completion/(耗时−首字)`，并用 tooltip 标出两种口径（含首字/扣首字）。因 `use_time` 仅整秒精度、对短请求做减法易下溢，后端在日志 `other` 新增精确总耗时 `lat`（毫秒，与 `frt` 同 `StartTime` 基准，写于 `GenerateTextOtherInfo`）；前端在整秒窗口 ≤0 时回退用 `lat` 重算，避免不显示。**仅改动后新产生的日志**带 `lat`，历史日志回退整秒逻辑。

### 优化 Changed
- **性能监控与过载保护解耦**：`monitor_enabled` 现在只控制系统资源指标采集；新增 `monitor_overload_samples` 数字配置，`0` 表示关闭过载保护，正数表示需要连续多少次（每 5 秒一次）采样超过 CPU/内存/磁盘阈值后才拒绝新的 Relay 请求。CPU、内存、磁盘分别维护连续超限计数，任一资源恢复到阈值内即清零；默认过载保护关闭。性能监控和过载保护配置均支持热更新。
- **概览与数据看板展示全部活跃模型成功率**：两个性能指标区域取消请求量前 6 个模型的展示限制，显示最近 24 小时有访问记录的全部模型；汇总查询包含统计窗口内所有分组的流量，不因当前分组配置变化遗漏模型。成功率颜色统一为 `≥99.5%` 绿色、`[99%,99.5%)` 黄色、`[95%,99%)` 橙色、`<95%` 红色。
- **客户折扣停用默认折扣**：客户折扣统计只使用命中的系列（模型名前缀）折扣；未命中任何系列时按 `1` 统计。客户管理页面移除默认折扣字段及列表列，历史 `default_discount` 数据库列保留用于兼容但不再展示或参与数据看板、日志和分流图中的客户折扣金额计算。
- **通用日志筛选主行宽度微调**：24 列栅格下时间范围 `8→7`、类型 `5→2`，腾出的宽度并入分组 `5→9`（模型保持 6），使分组下拉更宽松。
- **模型名称 / 分组 / 客户名称 / 客户ID 下拉支持清除**：四个 `Combobox`（走 legacy `ComboboxInput`）新增可选 `clearable`——有值且未展开时显示清除（X）按钮，一键把该筛选置空；默认关闭，不影响其他下拉框。

## [2026-10-25]

### 新增 Added
- **使用日志筛选支持多值正选/反选（Usage Log Multi-value Include/Exclude Filters）**：渠道、模型、用户名、令牌名、分组支持逗号分隔的多值筛选，并可通过 `=` / `≠` 切换正选或反选；筛选模式同步保存在 URL，列表与统计使用同一组参数。后端统一使用 `IN`/`NOT IN` 与 `LIKE`/`NOT LIKE` 条件，兼容 SQLite、MySQL、PostgreSQL 及 ClickHouse；新增后端回归测试覆盖组合筛选与 ClickHouse LIKE 转义。
- **客户统计图表改用 SVG 渲染**：客户统计 VChart 显式使用 `renderMode: 'svg'`，提升图表在当前页面环境中的渲染稳定性；补充 `QuotaDataItem.metric_denominator` 类型定义，匹配客户统计数据结构。
- **分流图悬浮提示补充金额口径**：在原有额度、Token、请求数、占比之外，新增渠道折后金额和客户折后金额；两项金额均按当前悬浮节点或链路对应的聚合数据展示。占比仍按当前分流口径的链路值除以当前筛选范围内所有首跳链路值之和计算。
- **客户统计金额支持四种口径**：客户统计页新增「客户折后金额 / 渠道折后金额 / 利润金额 / 利润率」切换。利润金额按客户折后金额减渠道折后金额计算，利润率按汇总利润除以汇总渠道折后金额计算；未配置客户在客户折后金额口径下继续使用渠道折后金额兜底。
- **明细错误新增「客户」列（Per-signature Customer Breakdown）**：明细错误统计页为每个签名/分组补充**按客户聚合**的次数分布列（与「模型」「渠道」同款 badge 聚合，signature 与 short_name/attribution 分组两种视图都有）。聚合依据为日志 `other.customer_id`（下游 Trace-Tag 解出，无客户信息的行不计入该列）。前端展示格式为 **`客户ID(客户名)`**，客户无对应配置名时只展示 `客户ID`；每个 badge 带该客户在本签名下的出现次数 `×N`、按次数降序、悬浮 tooltip 看全文。数据层：`errorSignatureOther` 解析 `customer_id`，`ErrorSignatureStat`/`ErrorGroupStat` 新增 `Customers map[string]int64` 在两条聚合路径累计；控制器 `errorStatItem` 新增 `customers`（`{customer_id,customer_name,count}`），客户名经 `model.CustomerName(id)` 查内存缓存解析（未配置则空）。零迁移、复用现有 `other` 字段，不新增列、不改表结构。注意：**客户名取自当前缓存**而非写时快照，且仅对带 `customer_id` 的错误日志生效。

### 优化 Changed
- **明细错误分类编辑交互调整**：移除简名/归因单独的清除按钮，统一通过编辑下拉框完成修改，减少重复操作入口。
- **明细错误「查看日志」下钻改为新标签页打开**：下钻到使用日志由原地跳转改为 `window.open(..., '_blank')` 新标签页打开，统计视图保持原位，便于对照排查（`router.buildLocation` 构造目标 URL）。

## [2026-10-24]

### 新增 Added
- **数据看板新增「客户统计」分区（Customer Statistics Dashboard，管理员）**：复制「用户统计」为管理员专属的「客户统计」子页，统计维度为**客户**（下游 Trace-Tag 解出的 `customer_id`），提供「客户消耗排行」「客户消耗趋势」两图，并可在页面内切换**按客户名 / 按客户 ID** 分组。金额口径固定为「客户折扣后金额」（其他口径在此页无意义）：已配置折扣的客户用 `customer_stat_quota`；**未配置客户名/折扣的客户回退用渠道折后金额 `channel_stat_quota`，标签追加「(channel discount fallback)」标注**——该回退**仅作用于本页展示层**，全局 `customer_stat_quota` 计算/含义不变（模型调用分析/分流/用户统计不受影响），兜底对「按客户名」「按客户 ID」两种视图都生效。数据层：`QuotaData`（`quota_data` 表）新增 `customer_id` 维度列（`index`/`size:64`/`default:''`，主库 `AutoMigrate` 自动补列），穿入写入链路——`RecordConsumeLog` 从 `other.customer_id` 取值随 `LogQuotaData` 落入，内存聚合 key 增加第 9 维，`SaveQuotaDataCache`/`increaseQuotaData` 的 `WHERE` 同步加 `customer_id`；新增 `GetQuotaDataGroupByCustomer`（按 `customer_id, created_at` 聚合、仅取 `customer_id <> ''`）与接口 `GET /api/data/customers`（`AdminAuth`），控制器逐行用 `model.CustomerConfigured`/`CustomerName`（名空回退=客户ID）富集出客户名与「是否已配置」。前端新增 `dashboard` 的 `customers` 分区（adminOnly）与 `components/customers/customer-charts.tsx`，`processCustomerChartData` 把每客户行拍平后复用 `processUserChartData`（后者新增可选标题入参，避免重复 VChart spec）；i18n 新增 8 个 key 补齐 en/zh/fr/ja/ru/vi。注意：**上线前的历史 `quota_data` 行 `customer_id` 为空**，客户统计只反映部署后产生的数据。
- **数据看板金额口径扩展：消耗分布/分流/用户统计可切「渠道折后金额·客户折后金额」（Dashboard Stat-Amount Calibers，管理员）**：把每条消耗日志算出的两个「仅统计」金额（渠道分组倍率折后 `stat_quota`、客户折扣后 `customer_stat_quota`）汇总进数据看板，在三处提供口径切换（均**仅管理员**，总额度口径对所有人保留）。(1) **模型调用分析 → 消耗分布图**新增「总额度/渠道折后金额/客户折后金额」口径切换，并在统计卡下方新增两张管理员金额卡；(2) **分流图**「流量宽度口径」在「按额度/按 Token/按请求数」后新增「按渠道折后金额/按客户折后金额」；(3) **用户统计**新增「按额度/按客户折后金额」切换。数据层：`QuotaData`（`quota_data` 表）新增 `channel_stat_quota`/`customer_stat_quota` 两列（主库 `AutoMigrate` 自动补列），`RecordConsumeLog` 结算时随 `LogQuotaData` 落入，`GetAllQuotaDates`/`GetQuotaDataBy*`/`GetFlowQuotaData`/`GetQuotaDataGroupByUser` 的聚合 `SUM` 带出。前端 `processChartData`/`processUserChartData`/分流 `rowMetrics`+`metricValue` 统一加「口径」参数取值。注意：两列为新加，**上线前的历史 `quota_data` 桶为 0**，切到两折后口径时历史区间偏小/为 0，总额度口径不受影响。
- **明细错误新增「解释」列（Per-signature Explanation/Note）**：明细错误统计页「围绕明细错误」视图新增管理员可内联编辑的「解释」列——一段**按签名**的短文本备注，用来补充「为什么这样标注（简名/归因）」。短文本截断展示、**鼠标悬浮 tooltip 看全文**。按签名哈希与简名/归因同存于别名表（新增 `note` 列 `varchar(255) default ''`，零迁移沿用现有表、随同一缓存与多实例周期同步生效），`/api/error-alias` upsert 支持 `note`，三者（简名/归因/解释）至少其一非空，全清空则删除该行。

### 优化 Changed
- **消耗日志「已应用参数覆盖」(`other.po`) 按数组下标折叠缩略（Param-Override Audit Line Folding）**：同一条参数覆盖规则命中数组多处时，原本会按每个下标各记一行（如 `regex_replace messages.0.role …` 一直到 `messages.55.role`，几十上百行几乎相同的摘要），既撑大消费日志 `other.po`，也拉长后端 `record consume log` 文本与 DB 存储。现在在 `relay/common/override.go` 落库前（`info.ParamOverrideAudit` 赋值处）用新增的 `foldParamOverrideAuditLines` 折叠：把「除数字数组下标外完全相同」的行用区间记法合并，如 `regex_replace messages.[1-9,11,15].role from ^developer$ to user`、连续区间 `messages.[0-55].role`。**多个下标位置同时变化**（如 `delete messages.M.content.N.endpoint_signature`）按维度逐层折叠（内层优先、逐轮迭代直到收敛），且**无损**——绝不臆造未真实出现的下标组合：例如 `messages.{0:[0-2], 1/3/7:[0-1], 2/4/5/6:[0]}` 折成 `messages.0.content.[0-2]` / `messages.[1,3,7].content.[0-1]` / `messages.[2,4-6].content.0` 三行，而非错误的笛卡尔积 `messages.[0-7].content.[0-2]`；对角集（`messages.0.content.0` + `messages.1.content.1`）因无法无损合并而原样保留。无数字下标的行原样透传。前端 `parseAuditLine`（按首个空格切分 action/content）对折叠串完全兼容，**前端零改动**；仅影响之后新写入的日志，旧日志不变。
- **归一化默认规则新增 `vendor-prefixed-id`**：默认规则集加一条通用规则 `\b(toolu|rs|resp|msg|item|fc|call|chatcmpl|req)_[A-Za-z0-9_]{6,}` → `${1}_*`，把带下划线前缀的厂商资源 id（如 Anthropic `toolu_bdrk_…`、OpenAI Responses `rs_…`）收敛、保留前缀。置于 `uuid` 之后、`long-hex` 之前（裸 hex 规则因 `\b` 边界抓不到 `前缀_hex`，故需单独处理）。仅影响**无自定义规则集**的部署与之后新产生的签名。
- **归一化默认规则新增 `encrypted-content-blob`**：默认规则集再加一条 `(encrypted content ).*?( could not be verified)` → `${1}*${2}`，把 OpenAI「加密内容无法校验」错误里 `The encrypted content … could not be verified` 中间的变长 blob（含上游网关 `头...尾` 截断形态、不可靠的 `gAAA` 前缀、`for item rs_…` 等任意内部形态）整体收敛为一行。置于 `strip-newapi-request-id` 之后、`uuid` 之前。仅影响无自定义规则集的部署与之后新产生的签名。
- **明细错误下钻改用 `error_signature_hash` 直列过滤**：既然错误日志已把归一化指纹双写进 `logs.error_signature_hash` 真列，「明细错误」下钻到使用日志的过滤从 `other` JSON 的跨方言 `LIKE` 改为 `logs.error_signature_hash = ?`（走索引、更快）。注意：**列加上之前产生的历史错误日志**仅在 `other` 里有指纹、列为空，用列过滤查不到它们；新日志不受影响。
- **简名/错误归因下拉候选改为全量已配置值**：明细错误页编辑「明细错误类型/错误归因」的下拉候选，此前只取**当前列表**里出现过的值，导致本次结果里没出现的旧配置值选不到。现额外拉取 `GET /api/error-alias` 全量别名，候选为「(归因先放 4 个种子) → 所有已配置别名值 → 当前行额外值」去重合并；增删别名后一并失效 `error-aliases` 查询，新值即时进下拉。
- **明细错误页工具栏布局调整**：三个「围绕…」视图切换按钮移到表格上方同一行的左侧，「推荐规则」「编辑归一化规则」并排置于该行右侧；上方仅保留筛选栏。
- **侧边栏「明细错误统计」改名为「明细错误」**：侧边导航项复用已有 `Detailed Error` 文案（zh=明细错误），缩短占位；页面标题仍为「明细错误统计」。
- 通用日志筛选主行改用 24 列栅格，使时间范围与类型两项都能完整容纳（`common-logs-filter-bar`）。
- **日志 `other` 字段「提列」首例：`error_signature_hash` 真列 + `other` 双写（Log other→column Dual-Write）**：把错误日志的归一化指纹从「仅存 `other.error_signature_hash`」改为**本机 `logs` 表真列 + 保留 `other` 双写**，为后续按列高效过滤/分组打基础。`model.Log` 新增 `ErrorSignatureHash`(`varchar(64)`，仅错误日志有值)，`RecordErrorLog` 在保留 `other` 那份的同时写入新列；本机 ClickHouse 建表 DDL 加列并在 `migrateClickHouseLogDB` 用幂等 `ALTER TABLE logs ADD COLUMN IF NOT EXISTS` 给存量实例补列（先迁列后写值），MySQL/SQLite 走 `AutoMigrate` 自动补列。**读路径不动**（`error_stats`、`GetAllLogs` 的 `error_signature_hash` 过滤仍解析 `other`），故新老行都正常、无需 backfill 或双读兼容查询。**决策：提列后不从 `other` 移除**——提列字段都很短，真列与 `other` 双份开销很小；留在 `other` 即无缝随 `logshipper.Row` 的 `other` 继续同步到远端 ClickHouse 集群，**远端表零改动**、不触碰 ck-ingest/Flink 链路。`logshipper.Row` 未加列。此策略适用于后续所有 `other`→真列的字段提升。详见 `docs/log-field-columnization.md`。
- **原始/折扣金额 5 字段也提列（Stat Amounts → Columns，消耗日志）**：沿用同款双写，把结算时算出的「仅统计」金额从 `other` 提升为本机 `logs` 真列——`RawQuota`/`StatQuota`/`CustomerStatQuota`(CH `Int32`)与 `StatGroupRatio`/`CustomerDiscount`(CH `Float64`)，全部 `DEFAULT 0`。`RecordConsumeLog` 从 `params.Other` 取 `raw_quota`/`stat_quota`/`stat_group_ratio`/`customer_stat_quota`/`customer_discount` 写入对应列并**保留 `other`**（缺键读取得零值，`customer_*` 仅带客户 id 的请求才有）；CH 存量实例由 `migrateClickHouseLogDB` 的 `ADD COLUMN IF NOT EXISTS` 列表循环补齐，MySQL/SQLite 靠 `AutoMigrate`。读路径不动（金额统计 `SumLogStatAmounts` 与详情弹窗仍读 `other`），`logshipper.Row` 未加列、远端零改动。详见 `docs/log-field-columnization.md`。
- **使用日志金额统计改用直列 `SUM`（Stat Amounts Read via Columns）**：既然金额已双写进真列，`SumLogStatAmounts` 从「扫 `other` 并 Go 累加（10 万行上限）」改为 DB 直接 `SUM` 列——`COALESCE(SUM(stat_quota),0)` / `SUM(customer_stat_quota)`，利润用 `SUM(CASE WHEN customer_discount>0 THEN customer_stat_quota-stat_quota ELSE 0 END)`（以 `customer_discount>0` 作「有客户」判据，等价旧逻辑按 `customer_stat_quota` 键是否存在）。去掉 10 万行扫描上限、改为全量聚合，跨 SQLite/MySQL/PG/ClickHouse 通用。注意：**列加上之前的历史消耗日志**金额只在 `other`、列为空(0)，按列 `SUM` 会把它们计 0；新日志不受影响。详情弹窗仍读 `other`。

### 修复 Fixed
- **客户统计利润率排行修正**：修复客户统计按利润率排行时将多个时间桶的单桶利润率直接相加，导致出现超过 100% 的错误。现在先按客户（趋势图按客户与时间）汇总利润和渠道折后金额，再计算利润率。
- **归一化规则集多实例不同步（Error Signature Rules Cross-Instance Sync）**：归一化规则与普通 K-V 配置不同——它被**编译进进程内缓存**（`service.signatureRules`），仅在启动时和「保存规则的那台实例」的 HTTP handler 里重新编译；其他实例虽经 `SyncOptions` 把新规则值轮询进 `OptionMap`，却从不重新编译，于是一直用旧规则。表现为：保存的那台产出新签名（如 `content.*`），其他实例仍产出旧签名（如 `content.8`），同一类错误并存两种签名。修复：`service.SyncErrorSignatureRules(frequency)` 按 `SYNC_FREQUENCY` 轮询 `OptionMap` 的 `ErrorSignatureRules`，**值变化时才**重新 `ApplyErrorSignatureRulesJSON`（带变更守卫避免无谓重编译；option 为空则回落默认规则），在 `main.go` 与 `SyncErrorAliasCache` 等并列启动。重启后在任一实例改规则，其他实例约 2×`SYNC_FREQUENCY` 内自动跟上，不再需逐台重启。
- **数据看板 `quota_data` 落库吞错且谎报成功（Dashboard Quota Data Silent Write Failure）**：`SaveQuotaDataCache`（`model/usedata.go`）此前把 `Create` 插入错误完全丢弃，且无论成功与否都恒打印「保存数据看板数据成功，共保存 N 条」（N 为缓存条数），导致 `quota_data` 真写不进去时在日志里完全隐身——数据看板/分流/模型调用分析静默无新数据，却看不到任何报错（本次排障一度据此误判）。现改为：`Create`、`increaseQuotaData` 的错误均检查并以 `common.SysError` 记录；`First` 查询区分 `ErrRecordNotFound`（正常「新小时桶」→插入）与真实 DB 错误（此时行是否存在未知，贸然 `Create` 可能重复计数，故记录并跳过本条）；结尾按**实际成功条数**记日志，一旦有失败改用 `SysError` 打印「成功 X 条，失败 Y 条（共 Z 条）」。`increaseQuotaData` 签名由无返回值改为返回 `error`。**仅提升可观测性**，不改写入/去重语义，也未加重试（失败条目增量仍随本轮缓存清空丢弃，与原行为一致）。

## [2026-10-23]

### 新增 Added
- **系统行为页补齐三项运维开关前端（System Behavior Options Frontend）**：为此前仅有后端的三个选项补齐「系统设置 → 运维 → 系统行为」前端——「调试模式」(`DebugEnabled`，即时生效无需重启)、「强制 1h 提示缓存 TTL」(`extended_cache_ttl_setting.rules`，Claude，按 分组×模型 规则命中，留空关闭)、「禁用元数据注入」(`metadata_injection_disable_setting.rules`，按 分组×模型 规则禁用 Claude `metadata.user_id/session_id` 注入，留空则全量注入)。两条规则用「分组 + 模型（逗号分隔、`*` 通配）」行编辑器维护，保存统一走 `/api/option/`，`.rules` 以 JSON 字符串下发；后端逻辑不变。
- **客户配置与错误简名映射的多实例缓存周期同步（Cache Sync Across Instances）**：`main.go` 启动 `SyncCustomerCache`/`SyncErrorAliasCache` 两个 goroutine，按 `SYNC_FREQUENCY` 从 DB 轮询重建「客户映射/折扣」与「错误签名→简名」内存缓存（对齐 `SyncOptions`/`SyncChannelCache`），使在另一实例上的新增/修改能在一个同步周期内传播到本实例，不再仅在处理写操作的那个实例生效；`frequency<=0` 则禁用。
- **错误归因维度（Error Attribution）**：在「明细错误」基础上新增与「简名(short_name)」平级的管理员可配置维度「错误归因(attribution)」——按签名哈希映射，`/api/error-alias` upsert 支持同时写 `short_name`/`attribution`（二者至少其一），缓存 `errorAliasEntry{ShortName,Attribution}` 由 `ResolveErrorAttribution` 读取；明细错误统计页新增按「错误归因」分组模式(`group_by=attribution`)与可内联编辑的归因列。零迁移沿用现有别名表（新增 `attribution` 列，`default:''`）。
- **使用日志用量统计区新增三项金额（管理员）+ Prometheus 金额指标**：通用日志统计条在「用量/RPM/TPM」右侧新增「渠道折后金额/客户折扣金额/利润」三项（仅管理员「全部」视图、受敏感信息掩码控制、响应全部筛选条件）。因三项金额只存在每条日志 `other` JSON、无可 SUM 列，后端 `SumLogStatAmounts` 按与列表一致的筛选扫描匹配日志并在 Go 累加（10 万行上限保护，复用抽出的 `applyAdminLogFilters`）；`/api/log/stat` 返回 `channel_stat_quota`/`customer_stat_quota`/`profit_quota`。同时新增 Prometheus 指标（quota 单位，可除以 `newapi_quota_per_unit` 得美元）：`newapi_channel_stat_quota_total`、`newapi_customer_stat_quota_total`（Counter）与 `newapi_profit_quota`（Gauge，利润可为负故用 Gauge、重启清零），均带 `{model,group,channel}`，在结算点落、不影响实际计费。

### 优化 Changed
- **系统行为页危险开关增加前端防护**：「演示站点模式」「自用模式」禁止开启——尝试开启被拦截并弹浮窗提示；「即使未配金额也允许模型访问」关闭时弹二次确认，提示关闭后未配价模型请求将被拒绝、在用的未定价模型会立即不可用的风险。
- **客户折扣改为按模型系列（前缀）配置（Customer Series/Prefix Discounts）**：把原先「按精确模型名」的客户折扣改为「按模型名前缀」的一组**系列折扣**（如 `claude` / `gpt` / `gemini` / `grok`），并去掉精确模型折扣；取折扣时按**命中模型名的最长前缀**生效，无命中则回退客户级默认折扣 `default_discount`，仍为“仅统计不改扣费”的乘数。客户页列表「系列折扣」列按前缀字符串排序**默认平铺展示**（内容过长截断、悬浮看全文），编辑抽屉改为「前缀 + 折扣」动态行。数据层新增 `prefix_discounts`（TEXT，JSON `前缀→折扣`）；旧列 `model_discounts` 采用**保留悬空列**策略——代码已彻底不再引用，不做重命名/复用/DROP（因 `AutoMigrate` 无法表达列重命名、跨库 `RENAME COLUMN` 语法不一致、且新旧语义不同），跨三种库零风险。`default_discount` 列不变。详见 `docs/customer-management-and-discount.md`。
- **通用日志筛选：客户名称/客户ID 改为下拉 + 布局调整（Log Filter Customer Dropdowns）**：管理员通用日志筛选新增「客户名称」下拉、「客户ID」由文本框升级为下拉（候选取 `GET /api/customer`，均保留手动输入任意值），并各含「其他（无客户信息）」项——选中发送哨兵 `__none__`，后端 `applyTraceTagFilters` 据此匹配**无任何客户信息**的日志（客户名按写时快照字符串 LIKE 匹配）。客户名称 / 客户ID / 下游请求ID / 下游TraceTag 四项统一移到高级筛选区**最后一行**；主筛选行时间范围收窄、类型加宽；「错误关键字」移到高级区**单独末行**（避免其随类型选择性出现时挤动其他框）。从「明细错误」下钻到使用日志时，筛选栏新增只读「错误签名」项回显当前过滤条件（随 `errorSignature` 透传、可被重置清除）。
- **计费详情金额行调整**：「折扣后金额」更名为「渠道折后金额」(带 `(x分组统计倍率)`)；「客户折扣后金额」展示 `(x客户折扣)`（后端新增写 `other.customer_discount`，按客户×模型解析）；在其后新增「利润金额」行＝客户折扣后金额 − 渠道折后金额（二者都存在时显示，可为负）。
- **Prometheus `newapi_error_total` 新增 `attribution` 标签**：与 `short_name` 并列记录错误归因（空值回退 `uncategorized`，低基数、不受 `ERROR_METRIC_HIGH_CARD_LABELS` 开关影响）；落点取 `model.ResolveErrorAttribution(signatureHash)`。注意该指标 series schema 多一标签，历史与新 series 会并存。

### 修复 Fixed
- **「编辑归一化规则」弹窗在明文 HTTP/内网 IP 下白屏 500（Error Signature Rules Dialog Crash）**：弹窗生成规则行的客户端 id 用了 `crypto.randomUUID()`，而该 API 仅在**安全上下文**（HTTPS 或 localhost）下有定义；经 `http://<内网IP>` 访问后台时 `crypto.randomUUID` 为 `undefined`，弹窗一打开即抛 `TypeError`、被前端 error boundary 接管渲染成「500」页（后端 `/api/log/error-signature-rules` 实际返回 200，并非服务端 500）。改用项目已在用的 `nanoid()`（底层 `crypto.getRandomValues`，明文 HTTP 下亦可用）。全局仅此一处使用 `crypto.randomUUID`。需重新 `bun run build` 前端生效。

## [2026-10-22]

### 新增 Added
- **客户管理与客户折扣（Customer Management & Discount）**：以「下游 Trace-Tag 日志」为基础的独立新功能。新增管理员「客户」页面（与「用户」平级，`/customers`，admin 守卫）维护「客户ID（唯一索引）→ 客户名」映射 + 备注 + `(客户×模型)` 折扣（含客户级默认折扣），后端 `/api/customer`（AdminAuth）+ `model.Customer` 表（`model_discounts` 以 JSON 存 TEXT 列，零第二表）+ 内存缓存（变更重建，热路径只读内存）。消耗日志侧：详情展示「客户名」（trace-tag 解出 `customer_id` 后就近查缓存、**写时快照**，无映射则空）；计费详情新增「客户折扣后金额」=**原价（分组倍率=1）× 客户折扣**（复用渠道统计金额的 `finalizeTextQuotaForGroupRatio`，走统一 quota 取整/clamp），写入 `other.customer_stat_quota`，仅对解出客户ID的请求落库。折扣为乘数（`0.8`=8折）、**仅统计不改实际扣费**（扣费口径逐分不变，text_quota 固定值回归用例全绿）。`customer_id` 经 `common.TraceTagFromContext` 一次解码缓存到 gin.Context，结算与日志两处共用。详见 `docs/customer-management-and-discount.md`。
- **下游网关 Trace-Tag 日志记录与检索（Downstream Trace-Tag Logging）**：链路为「客户 → 下游网关 → 本层 new-api → 上游网关」，下游网关在请求头 `x-bce-aigw-trace-tag` 带一个确定性认证加密（SIV：`HMAC-SHA256(macKey, 明文)[:16]` 作 IV + `AES-256-CTR`，base32 无填充）的值，本层解密还原出「下游 request_id」与「客户 ID（下游 `api_key_id`，可空＝公开模式）」，连同原始 tag 一并写入**消耗日志(type=2)与错误日志(type=5)** 的 `other` JSON（`downstream_trace_tag`/`customer_id`/`downstream_request_id`，零迁移、不加列），在日志详情**管理员可见**，并支持按这三者检索（`GetAllLogs`/`GetUserLogs` 新增 query 参数，`other` 做跨方言 `LIKE`，元字符转义）。子密钥由 `AIGW_TRACE_TAG_KEY`（64 位 hex 主密钥）经 HKDF-SHA256 派生（`sync.Once` 缓存）；密钥未配置/非法时打一次 `SysError` 并禁用解码、**不影响启动**；认证失败只保留原始 tag、绝不静默解成错误 ID；解码在热路径仅微秒级、且全程不影响日志写入。注意：`other` 无索引为全表扫描，检索需配合时间范围收窄。详见 `docs/downstream-trace-tag-logging.md`。
- **日志按错误关键字筛选（Log Filter by Error Keyword）**：通用日志筛选栏「高级筛选」区新增「错误关键字」输入框，对日志 `content` 列做包含（LIKE）匹配，**仅在日志类型选为「错误」时才展示、才生效**。错误文本由 `RecordErrorLog` 写入 `content`，故按关键字排障等价于 `content LIKE %关键字%`；用户输入的 LIKE 元字符会被转义后再包裹 `%...%`。前后端双保险——前端仅在类型=错误时渲染并在切换类型时清空，后端 `GetAllLogs` 仅当 `logType == LogTypeError` 且关键字非空时才追加条件，避免误触发全表扫描。跨方言 LIKE 由 `model/log.go` 的 `errorContentLikeCondition` 统一构造（SQLite/MySQL/PostgreSQL 用 `ESCAPE '!'`，ClickHouse 用反斜杠转义）。按范围仅管理端 `GetAllLogs` 支持；零迁移、不加列。注意：只命中错误日志、`content` 无索引为全表扫描（已被类型+时间范围收窄）、不影响顶部用量/RPM/TPM 统计。详见 `docs/log-filter-error-keyword.md`。
- **明细错误统计（Detailed Error Stats）**：把「归一化后的错误串」称为**签名**，并允许管理员按**简名（明细错误类型）**归类（多签名→一简名，未配置归「未分类」），便于把隐藏在 400 等状态码里的各类错误固定下来统计/检索/对外暴露。写错误日志时（`processChannelError`）实时归一化：`Content` 改存**未脱敏原文**（`err.ErrorWithStatusCode()`，响应仍脱敏），并把签名与其 `sha256` 指纹写入日志既有的 `other` JSON（`error_signature`/`error_signature_hash`）——**复用现有可扩展字段、不新增列、不改表结构**，对 SQLite/MySQL/PostgreSQL/ClickHouse 全部日志库零 schema 变更。归一化是一组**有序纯正则规则** `{name,pattern,replacement,enabled}`（RE2，支持 `$1`），默认删 `(request id:…)` 后缀、掩 UUID/长 hex/按 key 的 request id/ISO 时间戳；规则集可在页面编辑并持久化为 Option 行 `ErrorSignatureRules`，启动时应用、空则用默认，**顺序有意义、仅对之后日志生效**。「使用日志」同级新增管理员页「明细错误统计」：按签名聚合展示次数/占比/首末时间/状态码/模型·渠道 chips、内联编辑简名、「推荐规则」（基于实际签名给候选正则）、下钻复用使用日志列表（`GetAllLogs` 新增 `error_signature_hash` 精确过滤）。Prometheus 新增 `newapi_error_total{short_name,model,channel,status,protocol,stream,customer}`，按当时简名累加、`customer` 暂用 username，高基数标签（model/customer）由 `ERROR_METRIC_HIGH_CARD_LABELS`（默认开）开关。接口均 `AdminAuth`：`/api/log/error-stats`、`/api/error-alias`(增删查)、`/api/log/error-signature-rules`(读写+`/suggestions`)。注意：错误日志（type=5）默认非仅管理员可见，存原文依赖「本部署不开放普通用户」前提，若启用普通用户需将其加入 `adminOnlyLogTypes` 或改为原文只入 `admin_info`；聚合在缺省时间窗时会扫描并在内存聚合（上限 5 万行后截断），建议配合时间范围。详见 `docs/detailed-error-stats.md`。

## [2026-10-21]

### 新增 Added
- **渠道统计分组倍率（Statistics Group Ratio）**：为“分组倍率配成 0 以免扣费/耗额度”的免费透传渠道（如 `behind-cdnaigw`）解决统计金额归零的问题。在不改变实际扣费的前提下，结算时额外计算两个“仅统计”金额——**原始金额**（分组倍率=1 的原价）与**折扣后金额**（按渠道级 `stat_group_ratio`），写入每条消费日志 `other`（`raw_quota`/`stat_quota`/`stat_group_ratio`），并在日志详情弹窗展示。统计倍率存于渠道 `setting` JSON（`dto.ChannelSettings.StatGroupRatio`，零迁移；缺省/非正数按 1 即折扣=原价），在「渠道额外设置」组配置、渠道列表以 `×x` 展示。计费公式重构为“分组倍率=1 基数 × 任意倍率”三次求值，扣费口径与改前逐字节一致（原有固定值回归用例全绿）。注意：`tiered_expr` 计费渠道的这两个统计字段按标准公式计算、可能与表达式扣费不一致（不影响实际扣费）。详见 `docs/channel-stat-group-ratio.md`。
- **未配置价格模型放行开关（Allow Unset Model Ratio）**：在「系统设置 → 运维 → System Behavior」新增全局开关 `AllowUnsetModelRatio`（默认关）。开启后，价格与倍率均未配置的模型请求不再返回 `400`，而是按兜底倍率 `37.5 × 分组倍率` 计费放行（复用既有 accept-unset 分支，与「自用模式」「用户级 `AcceptUnsetRatioModel`」为「或」关系）。零迁移、不加列，走 `options` K-V 持久化；已配价/已配倍率模型不受影响。详见 `docs/allow-unset-model-ratio.md`。
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
