# 代理池功能解耦方案（Proxy Pool Decoupling Plan）

> 目的：代理池（全局代理表 + 渠道按别名引用 + 被动健康/故障转移 + TTFT/连接耗时动态分流 +
> 渠道统计 + Prometheus 指标）是本 fork 相对上游 QuantumNous/new-api 独立的一个功能。
> 当前主体逻辑已集中，但仍有约 10 处内联触点散落在上游核心文件里，rebase 时反复冲突。
> 本文把这些逻辑收敛进一个专属包 `service/proxypool`，并把上游触点压成少量「一行 hook」，
> 让 rebase 时上游核心文件几乎零冲突、主体逻辑在 rebase 永不触碰的新包里。
>
> 本文为执行计划，新会话可直接照做。**不改变任何运行时行为**，纯结构重构。

## 0. 执行前必读
- 这是**纯重构**：对外行为（选路结果、tp 改写、健康冷却、TTFT/连接分流、统计接口、指标）必须一比一不变。
- 相关既有文档：`docs/proxy-pool.md`（功能说明）、`CHANGELOG.md` 的 2026-11-01/02 段、记忆库 `project_proxy_pool.md`。
- 验证口径见本文 §6；不得以「能编译」替代测试。

## 1. 功能范围（哪些代码属于本功能）
已独立、rebase 低成本（保留/整体迁移，不散落）：
- `setting/system_setting/proxy_setting.go`（配置：ProxySetting/ProxyEntry/ProxyType/TtftRouting + 变更 hook）
- `service/proxy_health.go`（被动健康、`ResolveChannelProxy(Weighted)`、`RecordProxyConnectResult`、`ProxyHealthSnapshot`、`ProxyBehaviorForAlias`/`IsTpProxyAlias`、demote/order）
- `service/proxy_ttft.go`（TTFT + 连接耗时统计、闸门、加权选路、`ProxyTtftChannelStats`、prom 记录调用）
- `service/proxy_health_test.go`（测试）
- `controller/proxy.go`（`GetProxyHealth`）
- 前端 `web/src/features/system-settings/models/proxy-pool-section.tsx`、`web/src/features/channels/components/dialogs/channel-stats-dialog.tsx` 等
- `docs/proxy-pool.md`

## 2. 侵入上游核心文件的触点（要收敛的耦合）
以下为本功能嵌进上游/共享文件的内联改动（行号随代码漂移，以符号为准）：

1. `relay/channel/api_request.go` `doRequest`（**最重**，约 60+ 行）
   - 选路 `service.ResolveChannelProxyWeighted(...)`
   - 写 `info.UsedProxyURL/UsedProxyAlias` 并 `c.Set("used_proxy_url"/"used_proxy_alias")`
   - tp/force_http 改写：`service.ProxyBehaviorForAlias` + `applySchemeHeaderProxyRewrite`/`forceHTTPProxyScheme`
     （**这两个改写函数本体也定义在本上游文件里**，约 326-357，连同其单测 `api_request_test.go`）
   - httptrace 连接采样块（ConnectStart/TLSHandshakeDone/GotConn → proxyConnectDur/proxyGotConn）
   - `service.RecordProxyConnectResult(...)`（成功/失败/resp 为 nil 三处）
   - 通过 `service.GetHttpClientWithProxySettings(proxyURL, ...)` 构造带代理的 client
2. `relay/common/relay_info.go`：结构体加了 `UsedProxyURL` / `UsedProxyAlias` 两字段
3. `service/quota.go` / `service/text_quota.go`：结算点各一行 `RecordProxyTTFT(...)`
   （**注意**：这两个是 billing read-gate 文件，改动前需读 `.agents/rules/billing.md`；本重构只挪调用、不碰计费）
4. `controller/channel.go`：`GetChannelStats` handler；`validateChannel` 里 AWS(ChannelTypeAws=33)×tp 别名守卫
5. `controller/relay.go`：错误日志分支从 context 读 `used_proxy_url`/`used_proxy_alias` 写 `adminInfo["proxy"]`
6. `router/channel-router.go`：`GET /:id/stats` 路由；`/api/option/proxy_health` 路由（RootAuth）
7. `model/option.go`：`handleConfigUpdate` 对 `configName=="proxy_setting"` 调 `system_setting.NotifyProxyPoolChanged()`
8. `pkg/prom_metrics/metrics.go`：内联定义/注册/Record 了 `newapi_proxy_ttft_seconds{channel,proxy,model}`
   与 `newapi_proxy_connect_seconds{channel,proxy}` 两个直方图
9. `relaykit/dto/channel_settings.go`（独立 module，上游共享 DTO）：`Proxy` / `ProxyAliases` / `ProxyTtftRoutingEnabled`
10. `service/http_client.go`：`GetHttpClientWithProxySettings` / `ResetProxyClientCache` + 代理 client 缓存
    （**偏通用工具**，被多个 adaptor 复用，非本功能专属，原则上不动）
11. `service` 包 `init()`：`system_setting.RegisterProxyPoolChangeHook(ResetProxyClientCache)`

## 3. 不可消除的耦合（必须保留的薄缝）
- 中继出站路径 1 处（`doRequest`）：选代理 + tp 改写 + 连接埋点，只能在真正发请求处。
- 结算点（`quota.go`/`text_quota.go`）：TTFT 需在出首字/结算时观测。
- `relaykit/dto.ChannelSettings`：per-channel 配置必须随渠道存（relaykit 不能反向依赖 root）。
- 配置注册（`system_setting`）：已通过反射式 Register 隔离，成本近零。

## 4. 目标结构：`service/proxypool` 包
放在 `service/` 下（`service` 已被广泛引用，避免 import 环重排）。把 §1 的 `proxy_health.go`、`proxy_ttft.go`、
`proxy_health_test.go`，以及 §2.1 的 tp 改写函数 `applySchemeHeaderProxyRewrite`/`forceHTTPProxyScheme`（及其单测）
**整体迁入**该包。对外只暴露小而稳定的 API：

```
package proxypool

// 选路：返回本次应使用的代理（url/alias/type；alias=="" 表示直连/回退）
func Resolve(channelID int, s dto.ChannelSettings, model string) Decision

// 出站装配：按 Decision 改写 req(tp/force_http)、挂 httptrace，返回带代理的 client 和
// 一个在请求结束时调用的收尾函数（记录连接成功/失败 + 连接耗时）。
func InstrumentRequest(c *gin.Context, req *http.Request, d Decision) (client *http.Client, done func(err error))

// 观测：结算时记录 TTFT（内部判零/非流式跳过 + 写 prom）
func RecordTTFT(channelID int, model, alias string, seconds float64)

// 统计/快照（给 controller）
func ChannelStats(channelID int, aliases []string, ttftEnabled bool) []ModelRoutingStat
func HealthSnapshot() []AliasStatus

// 分类守卫（给渠道校验）
func IsTpAlias(alias string) bool

// 指标自注册（见 §5.8）
func RegisterMetrics(reg *prometheus.Registry)
```

`Decision` 内含 url/alias/proxyType/auth。`InstrumentRequest` 把当前散在 `doRequest` 里的
tp 改写 + httptrace + 连接记录全部收口，调用方只留 2~3 行。

## 5. 逐触点迁移步骤
5.1 `api_request.go`：删掉内联块，改为
```
d := proxypool.Resolve(info.ChannelId, info.ChannelSetting, info.UpstreamModelName)
client, done := proxypool.InstrumentRequest(c, req, d)
...
resp, err := client.Do(req); done(err)
```
tp 改写/埋点/连接记录随函数搬进包；`applySchemeHeaderProxyRewrite`/`forceHTTPProxyScheme` 及其单测移入包。
5.2 `relay_info.go`：**删除 `UsedProxyURL/UsedProxyAlias` 两字段**，改由 context key 承载（错误日志路径本就用 `c.Set`）。
    `proxypool` 定义自己的 context key，`InstrumentRequest` 写入、结算/错误日志读取。→ relay_info.go 不再被碰。
5.3 `quota.go`/`text_quota.go`：保留一行 `proxypool.RecordTTFT(...)`，但 alias/channel/model 从 context 或 relayInfo 取
    （若 5.2 改 context，则从 context 取，彻底不依赖被删字段）。结算语义与计费不变。
5.4 `controller/channel.go`：`GetChannelStats` 调 `proxypool.ChannelStats(...)`；AWS×tp 守卫改为注册一个渠道校验 hook
    （或调用 `proxypool.IsTpAlias`），避免逻辑写死在 `validateChannel` 内部。
5.5 `controller/proxy.go`：`GetProxyHealth` 调 `proxypool.HealthSnapshot()`；可把 `GetChannelStats` 也挪到这里。
5.6 `controller/relay.go`：错误日志改从 `proxypool` 的 context helper 读取（替代裸 `c.Get("used_proxy_url")`）。
5.7 `router/`：新建 `router/proxy-router.go` 集中注册 `/api/option/proxy_health` 与 `/channel/:id/stats`（或保留原组、仅换 handler 包）。
5.8 `pkg/prom_metrics/metrics.go`：给上游加一个**通用一次性缝** `func RegisterExtraCollectors(cs ...prometheus.Collector)`
    （或 `OnInit(func(reg))`）。把两个 proxy 直方图的定义/注册/Record 从 metrics.go 移入 `proxypool`，启动时自注册。
    → metrics.go 不再有 proxy 专属代码。
5.9 `relaykit/dto/channel_settings.go`：把 `ProxyAliases`/`ProxyTtftRoutingEnabled`（及 `Proxy`）收进一个嵌套
    `Proxy *ProxyChannelConfig` 子对象，集中成一处（子结构仍留在 relaykit；不能依赖 proxypool）。此项可选、收益一般。
5.10 `model/option.go`、`service` 的 `init()` hook：保持（已是一行 hook），或把 `ResetProxyClientCache` 注册移入 proxypool 的 `init()`。

## 6. 验证（必须实际执行并记录结果）
- `gofmt -w` 所有改动的 .go；根模块 `go build ./...`；relaykit `GOWORK=off go build ./...`（§5.9 碰 relaykit 时必跑）。
- `go test ./service/... ./pkg/prom_metrics/... ./setting/system_setting/...`，以及迁入后的 `proxypool` 包测试
  （把 `TestProxyFailover*`/`TestProxyBehaviorForAlias`/`TestProxyTtft*`/`TestProxyConnectLatencyRouting` 随包迁移并通过）。
- 数据库：本功能为进程内内存 + option JSON，无 schema/迁移，**不触发三库门禁**（如误碰 model/迁移再按 AGENTS 跑三库）。
- 前端若动到：touched 文件 oxlint 0 error；en/zh locale 若动则二者齐全；`bun run typecheck`
  （注：本环境 `bun check` 子命令缺失、`bun x tsc` 会卡基线类型，正常前端环境再跑）。

## 7. 必须保持的不变量（回归重点）
- `doRequest` 的连接错误语义：仅 `!GotConn` 记为连接失败；`ProcessShutdownFromContext` 命中时不记；resp==nil 分支。
- 连接耗时采样：dial+TLS、仅新连接（`!Reused`），复用连接不产生样本。
- TTFT：仅流式且已出首字（`FirstResponseSeconds()>0`）计入。
- tp(反代)：连接耗时不参与加权、固定第一档；AWS(33) 禁止引用 tp 别名（保留 `validateChannel` 前置拒绝、不落库）。
- 选路：连接冷却始终优先（不健康代理永不选）；TTFT/连接任一闸门触发即分流、两者都恢复才关；权重按两者乘积。
- 统计口径：概率=本节点实时、picks=本节点累计、重启清零；指标为 histogram 且与内存统计独立。
- i18n：前端新键仅 en/zh。

## 8. 分阶段（建议先做低风险试点）
- **阶段 0（试点，低风险）**：建 `service/proxypool` 包，整体迁入 `proxy_health.go`+`proxy_ttft.go`+tp 改写函数及其测试；
  `service`/`controller`/`relay` 暂以 `proxypool.*` 别名/转发调用过渡；跑全量 `go test ./service/...` 绿。
- **阶段 1**：§5.2 context 化 `UsedProxy*`，去掉 relay_info.go 改动；§5.8 prom 自注册缝，去掉 metrics.go 改动。
- **阶段 2**：§5.4/5.5/5.6/5.7 控制器与路由集中；§5.9 DTO 收敛（可选）。
- 每阶段结束更新 `docs/custom-modules-inventory.md`：列出「剩余薄缝」（relay 出站、结算、DTO 字段、prom 缝、config 注册），供后续 rebase 照搬。

## 9. 另一条更彻底但不建议现在做的路
用自定义 `http.RoundTripper` / `Transport.Proxy` 回调把「选代理+改写+埋点」塞进传输层，relay 代码零显式调用。
最干净，但 `Transport.Proxy` 的 per-request 选 URL 会改变现有「按解析出的代理 URL 缓存 client」的连接池语义，
