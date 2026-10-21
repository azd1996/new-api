# 代理池（Proxy Pool）方案文档

> 全局能力：在「系统设置 - 模型」维护一张全局代理表（别名 `alias` → 地址 `url`），各渠道通过**别名多选**引用一个或多个代理。请求时按运行时优先级顺序取**第一个当前健康（非冷却中）**的代理；别名列表为空或全部不健康时，回退到渠道原有的「代理地址」文本框配置。
>
> 代理健康**不依赖主动探测**：由真实上游请求的连接层结果被动采样（连接建立成功/失败、dial+TLS 耗时），连续失败触发降级并记录事件日志，冷却后半开重试。

---

## 1. 能力边界

- **别名引用 + 顺序故障转移**：渠道存一组代理别名；解析时按运行时顺序选第一个「非 unhealthy」的代理。无别名/全不健康 → 回退文本框 `Proxy`（向后兼容，老渠道零改动）。
- **被动健康，非主动探测**：不起后台探测 goroutine、不配探测目标。健康状态来自真实请求经代理的连接层结果。代价是「耗时」样本偏稀疏（见 §5）。
- **只覆盖主 relay 路径**：健康采样与「日志记录代理」目前只接在 `relay/channel/api_request.go` 的 `doRequest`（覆盖绝大多数 chat/completions 流量）。task 适配器、video_proxy、codex 等调用点仍直接读 `.Proxy`，未走别名解析（见 §9）。
- **运行时状态进程内**：每个代理的健康态、每个渠道的运行时优先级顺序都在**进程内存**，5 节点集群各节点独立收敛（可接受，不需要 Redis 一致）。
- **代理地址可能含凭证**：`url` 可形如 `http://user:pass@host:port`，故日志中代理信息放在 `admin_info`（仅管理员可见），后台日志只打别名不打 URL。

## 2. 端到端链路

```
渠道配置 proxy_aliases=[hk-1, hk-2] + 文本框 proxy=socks5://backup
    → doRequest: ResolveChannelProxy(channelId, aliases, fallback)
        → 运行时顺序取第一个非冷却中的别名 → 全局表查其 url
        → 都不可用 → 回退 fallback(文本框)
    → NewProxyHttpClient(url) 发请求（httptrace 采样连接耗时/失败）
    → RecordProxyConnectResult: 成功记耗时样本/清失败计数；
       连接失败累计达阈值 → 标 unhealthy + 冷却 + 该别名在所有渠道顺序里移到队尾 + 事件日志
    → 日志(消耗/错误)在 admin_info.proxy 记录实际使用的 {alias,url}
```

## 3. 存储与配置

- **全局代理表**：`setting/system_setting/proxy_setting.go`，模块名 `proxy_setting`，结构 `ProxySetting{ Proxies []ProxyEntry{Alias,URL,Description} }`。复用 `setting/config` 的反射式 option 持久化（存入 `option` 表的 `proxy_setting.proxies`，值为 JSON 数组），**无需改 model/controller、无 DB 迁移**。`Description` 仅为代理备注，只在代理池配置页展示，不参与解析、不进日志/渠道多选等其他位置。
- **渠道引用**：`dto.ChannelSettings.ProxyAliases []string`（json `proxy_aliases,omitempty`），与既有 `Proxy string` 并存，存在渠道 `setting` JSON 里。
- **前端**：
  - 代理表编辑：`web/default/src/features/system-settings/models/proxy-pool-section.tsx`（挂在「模型」分组，别名+地址多行增删 + 状态/耗时列）。
  - 渠道编辑：`channel-mutate-drawer.tsx` 新增「代理池别名」多选（选项来自代理健康接口的别名列表，可手动创建），`channel-form.ts` 负责 schema/默认值/解析/序列化。

## 4. 解析与优先级（ResolveChannelProxy）

`service.ResolveChannelProxy(channelID, aliases, fallback) (url, alias)`（`service/proxy_health.go`）：

- 归一化别名（去空、去重、保序）；为空直接返回 `(fallback, "")`。
- 取该渠道的**运行时顺序** `channelOrders[channelID]`：按「配置别名列表」的签名初始化/重建（管理员改了渠道别名列表 → 签名变 → 重建为新配置顺序）。
- 顺序遍历：第一个「在全局表里有非空 url」且「当前不在失败冷却中」的别名 → 返回其 `(url, alias)`。
- 全部不可用 → 返回 `(fallback, "")`（alias 为空表示用了文本框回退，不记健康）。

> `alias==""` 是回退/无代理的标记，调用方据此跳过健康采样。

## 5. 被动健康采样

状态三态：`healthy / unknown / unhealthy`。核心常量（`service/proxy_health.go`）：`proxyFailureThreshold=3`、`proxyCooldown=60s`、`proxyLatencyWindow=200`。

**采样来源**：`doRequest` 对 `req` 挂 `httptrace.ClientTrace`：
- `ConnectStart`/`TLSHandshakeDone` 计算**连接建立耗时（dial+TLS）**；`GotConn` 判断是否拿到可用连接。
- 请求返回后 `RecordProxyConnectResult(channelID, alias, connectLatency, connErr)`：
  - `connErr = (err != nil && 未 GotConn)` —— 只把「拿不到连接」判为代理故障；**上游 4xx/5xx 不算**（代理其实是通的）。

**状态机**：
- 成功：清零连续失败计数、`everSucceeded=true`、清冷却（半开成功即恢复）、若 `connectLatency>0` 记一条耗时样本（环形缓冲 200）。
- 失败：连续失败 +1；达到阈值(3) 且不在冷却中 → 置 `cooldownUntil=now+60s`、清计数、触发降级。
- **降级**（防抖、不自动抢回队首）：`demoteProxyAlias` 把该别名在**所有渠道**的运行时顺序里移到队尾，并 `common.SysLog` 一条「因代理访问失败，调整代理优先级」。
- **恢复**：冷却到期后该别名重新「非 unhealthy」可被选中（半开）；成功→healthy，失败→继续冷却。它停留在队尾，不自动回到队首。

**关键语义（坑）**：`unknown`（从未采样/半开）视为**可被选中**。若把 unknown 当不可用，新加/刚恢复的代理永远拿不到真实流量 → 永远 unknown → 冷启动与恢复双双死锁。

**耗时列语义**：取「连接建立耗时」的 P50/P90/P99（排除上游模型思考时间）。因 keep-alive 连接复用，只有新建连接时才有样本，故样本偏稀疏——这是被动方案的固有取舍（主动探测方案被明确放弃）。备用代理（空闲、无流量）没有样本，所以「按最快耗时把代理提到队首」这类优化未做（它必然需要主动测量空闲代理，与纯被动互斥）。

**管理端查询**：`GET /api/option/proxy_health`（RootAuth，`controller/proxy.go` `GetProxyHealth` → `service.ProxyHealthSnapshot`），返回全局表每个别名的 `status / p50_ms / p90_ms / p99_ms / samples / last_checked_unix`。前端代理表每行据此显示状态 + 百分位，15s 刷新。

## 6. 代理客户端缓存失效

`service.proxyClients` 按 `url` 字符串缓存 `*http.Client`。改某别名的地址后，新请求用新 url 自动建新 client（正确性无碍），但旧 url 的 client 会残留。为此：

- `setting/system_setting/proxy_setting.go`：`RegisterProxyPoolChangeHook` / `NotifyProxyPoolChanged`（钩子，避免 model→service 导入环，以 system_setting 为中介）。
- `service` 的 `init()` 注册 `ResetProxyClientCache` 为回调。
- `model/option.go` 的 `handleConfigUpdate`：`configName=="proxy_setting"` 时调 `NotifyProxyPoolChanged()`。

覆盖两条路径：管理员保存（controller 更新）、以及**所有节点周期性 `SyncOptions` 重载**——各节点拿到新配置时都清自己的 client 缓存。

## 7. 日志记录实际使用的代理

- `relay/common.RelayInfo` 增 `UsedProxyURL / UsedProxyAlias`，`doRequest` 解析后写入；并 `c.Set("used_proxy_url"/"used_proxy_alias")` 供无 relayInfo 的通用错误日志路径读取。
- **消耗日志**：`service/log_info_generate.go` 的 `appendUsedProxyAdminInfo` 把 `{alias,url}` 写到 `other.admin_info.proxy`（`GenerateTextOtherInfo` 内调用，覆盖 text/claude/audio/wss）。
- **错误日志**：`controller/relay.go` 错误分支从 context 读出并写 `adminInfo["proxy"]`。
- **可见性**：放在 `admin_info` 下 → 仅管理员可见（非管理员日志视图整体剥离 `admin_info`），因 url 可能含凭证。
- **前端展示**：`usage-logs/types.ts` 的 `admin_info.proxy?{alias,url}`；日志详情弹窗 `details-dialog.tsx` 新增「Proxy Used」行（仅 admin、非充值日志，显示 `别名 (url)` 或仅 url）。

## 8. 关键文件索引

- 后端
  - `setting/system_setting/proxy_setting.go` — 全局代理表 + 变更钩子
  - `service/proxy_health.go` — 健康态/百分位/运行时顺序/`ResolveChannelProxy`/`RecordProxyConnectResult`/`ProxyHealthSnapshot`，并在 `init()` 注册缓存失效钩子
  - `service/http_client.go` — `NewProxyHttpClient`（按 url 缓存）/`ResetProxyClientCache`
  - `relay/channel/api_request.go` — `doRequest`：解析代理 + httptrace 采样 + 记录结果 + 写 RelayInfo/context
  - `relay/common/relay_info.go` — `UsedProxyURL/UsedProxyAlias`
  - `dto/channel_settings.go` — `ProxyAliases`
  - `controller/proxy.go` + `router/api-router.go` — `GET /api/option/proxy_health`
  - `model/option.go` — `handleConfigUpdate` 触发钩子
  - `service/log_info_generate.go` / `controller/relay.go` — 日志写入代理信息
- 前端
  - `features/system-settings/models/proxy-pool-section.tsx` — 代理表编辑 + 状态/耗时列
  - `features/channels/.../channel-mutate-drawer.tsx` + `lib/channel-form.ts` — 渠道别名多选
  - `features/usage-logs/.../details-dialog.tsx` + `types.ts` — 日志展示「Proxy Used」

## 9. 未覆盖 / 后续

- **非主 relay 路径未接**：task 适配器、`video_proxy`、`codex`、`mjproxy`、`channel-billing` 等仍直接读 `channel.GetSetting().Proxy`，不走 `ResolveChannelProxy`、不记录健康与日志代理。如需全覆盖，逐个把这些调用点改为解析别名并上报结果。
- **按耗时挑最快提队首**：已确认不做——需主动测量空闲备用代理（迷你主动探测或 canary 分流），与纯被动方案互斥。若将来要做，作为可选的方案 B 独立实现。
- **运行时顺序进程内**：各节点独立，不跨节点一致；重启后顺序回到配置默认。若需集群一致，可将运行时顺序/健康态挪到 Redis。

## 10. 多节点收敛行为（已知、可接受）

**现象**：集群部署（如 5 节点 + 前置 NLB）下，把某个代理（如 proxyA）停掉后持续请求，会观察到「失败几次 → 成功一次 → 又失败几次 → 又成功几次 → 最终持续成功」的交替，而不是立刻全部切到 proxyB。

**原因**：健康状态与运行时优先级顺序是**每个进程内独立**维护的（见 §5、§9）。NLB 把请求轮询到不同节点，每个节点都要**各自**累计 `proxyFailureThreshold`（默认 3）次连接失败，才会在该节点把 proxyA 降级。收敛期内不同节点处于不同阶段：

- 已学会的节点 → 用 proxyB → 成功；
- 还没学会的节点 → 仍选 proxyA → 失败（并把这次连接失败透传给客户端）；

于是客户端看到成功/失败交替。等 5 个节点**各自**都降级 proxyA 后 → 全部走 proxyB → 持续成功（即最终稳定态）。叠加阈值 3，每个节点在收敛前最多把约 3 次失败透传出去，全集群约十几次可见失败后收敛。

**这是预期行为，非单机逻辑缺陷**。单进程内的切换是确定且稳定的：`service/proxy_health_test.go` 的 `TestProxyFailoverConvergesAndStays` 验证了「proxyA 连续失败达阈值后切到 proxyB 且后续不回弹」；`TestProxyFallbackWhenAllCooling` 验证了「全部别名冷却时回退文本框代理」。

**取舍与可选优化（当前不做，已接受现状）**：

- 收敛期的可见失败数 ≈ 节点数 × 阈值。若要减少，可降低阈值（§5 常量 `proxyFailureThreshold`），代价是对瞬时抖动更敏感（有 60s 冷却兜底）。
- 若要「停掉某代理后客户端完全无感」，需做**请求内即时故障转移**（连接层失败当次请求立刻改走下一个可用代理），这会消除收敛期的可见失败，但改动较大且流式仅能在首字节前切换。
- 若要「一个节点学会、全集群立即受益」，需把健康态/顺序放到 Redis 做跨节点共享。

