# 客户批量模型映射（Customer Batch Model Mapping）方案文档

> 渠道级能力：在渠道上额外维护一组「客户（customers）」，系统按 `基础模型 × 客户` 自动生成别名模型 `<model>_<customer>`，并同步写入渠道的 `Models`（使其可路由）与 `ModelMapping`（别名 → 真实模型，供上游改名）。
>
> 起因：`behind-cdnaigw` 是多客户共享渠道，下游网关把多个真实客户的流量汇聚上来，本层网关在渠道日志里无法区分下游客户；而给下游加「标识用户的请求头」依赖下游开发排期。本功能借助模型别名绕开该依赖——因为日志的 `model_name` 记录的是客户端请求的原始模型名，别名天然带客户信息。

---

## 1. 能力边界

- **仅用于「按客户区分日志/用量」**：日志 `model_name` = 客户端请求的原始模型名（`OriginModelName`），不是映射后的上游名。客户端请求 `claude-sonnet-5_customerA` → 日志即显示该名 → 按模型名即可区分客户。
- **别名必须双写**：`abilities` 表与内存路由缓存都只从 `Channel.Models`（CSV）构建，仅出现在 `ModelMapping` 的 key 不可路由。故别名既写 `Models`（可路由）又写 `ModelMapping`（改回真实模型）。
- **分隔符固定 `_`**：别名形如 `<model>_<customer>`。碰撞（真实模型名恰好等于「另一真实模型_某客户」）概率极低，由手工配置规避。
- **不改路由核心**：采用「保存时展开」，别名在管理员保存渠道时落库进 `Models`/`ModelMapping`，路由/缓存/abilities 代码零改动。
- **别名对外隐藏**：在渠道编辑页模型多选框与对外模型列表（`/v1/models` 等）均不展示别名，避免客户看到其它客户后缀；隐藏只影响「列出」，不影响路由与使用。

## 2. 端到端链路

```
客户端(modelA) → 下游网关(运维配置改名 modelA → modelA_customerA)
             → new-api: 命中别名 ability 选中渠道
             → ModelMappedHelper 改回 modelA 请求上游
             → 计费按 modelA（见 §4） → 日志 model_name = modelA_customerA
```

下游网关由用户用运维权限配置模型改名，真实客户侧模型名不变，**不依赖下游开发排期**。

## 3. 配置与存储

- 存储字段：`ChannelInfo.Customers []string`（`model/channel.go`）。`ChannelInfo` 自带 `Value()/Scan()`，序列化进既有 `channel_info` 列，**无 DB 迁移**。
- 展开入口：`(*Channel).ApplyCustomerModelExpansion(previousCustomers []string)`（`model/channel.go`）。
  - 以提交的 models 为基础模型，对每个 `(base × customer)` 生成别名；写回 `Channel.Models`（基础模型在前、再按模型分组追加别名，去重保序）与 `Channel.ModelMapping`（手动映射 ∪ `别名→base`）。
  - **幂等**：先按「历史 ∪ 当前」客户剥离已有别名再重算；`previousCustomers` 用于清理被移除客户的遗留别名。
  - 空客户且历史也为空时直接返回，不触碰 `Models`/`ModelMapping`。
- 接入点（`controller/channel.go`）：`AddChannel`、`UpdateChannel`、以及复制流程（`CopyChannel` 浅拷已展开的渠道，无需再展开）。`UpdateChannel` 原先用 DB 原值覆盖整个 `ChannelInfo`，已改为保留多 key 状态、仅当请求携带 `channel_info` 时取其中的 `customers`。

别名识别规则（前后端一致）：`m` 是别名，当且仅当存在客户 `c` 使 `m` 以 `"_" + c` 结尾、且去掉该后缀的前缀仍在模型集合内。

## 4. 计费（方案甲：通用回退）

计费与「未配置价格」拦截都按 `OriginModelName` 查价，别名未单独配价会被拒。为免逐别名配价：

- `resolvePricingModelName`（`relay/helper/price.go`）：本次请求发生了模型映射（`IsModelMapped`）、且请求名自身无任何价格配置时，改用映射目标名（`UpstreamModelName`）计价。
- 对**所有模型映射**通用，不止客户别名；`ModelPriceHelper` / `ModelPriceHelperPerCall` / tiered 分支统一使用该名。日志仍记原始名。
- `info.ChannelMeta` 可能为 nil，已判空回退到原始名。

## 5. 列表隐藏别名

- 后端：`InitChannelCache`（`model/channel_cache.go`）遍历所有渠道构建全局别名集；`FilterCustomerAliasModels` 过滤。经 `model/ability.go` 的 `GetGroupEnabledModels` / `GetEnabledModels` 生效，覆盖 `/v1/models`、Dashboard、可用模型等。只影响列出，不影响路由。依赖内存缓存开启。
- 前端：渠道编辑页模型多选框因「表单只回显基础模型」而天然不显示别名；全局模型来源也已被后端过滤。

## 6. 前端（仅 `web/default`）

- `features/channels/lib/channel-form.ts`：schema 新增 `customers: string[]`；`transformChannelToFormDefaults` 回显时从 `channel.models` / `channel.model_mapping` 剥离别名（只显示基础模型 + 手动映射），`customers` 取自 `channel.channel_info.customers`；创建/更新 payload 发送 `channel_info.customers`（仅 customers）与基础 `models`/手动 `model_mapping`，由后端展开。
- `features/channels/lib/model-mapping-validation.ts`：新增纯函数 `isCustomerAliasModel` / `stripCustomerAliasModels` / `stripCustomerAliasMappings`。
- `features/channels/components/drawers/channel-mutate-drawer.tsx`：模型映射与分组之间新增「客户批量模型映射」多选（`name='customers'`，`allowCreate`）。
- `features/channels/types.ts`：`channel_info.customers?: string[]`。
- i18n：6 语言（en/zh/fr/ja/ru/vi）新增 `Customer Batch Model Mapping` 等 4 个 key。

## 7. 典型用法

渠道基础模型 `modelA,modelB`，在「客户批量模型映射」填 `customerA,customerB`，保存后：

- `Models` = `modelA,modelB,modelA_customerA,modelA_customerB,modelB_customerA,modelB_customerB`
- `ModelMapping` 追加 `{modelA_customerA:modelA, ...}`（叠加在手动映射之上）
- 编辑页仍只显示 `modelA,modelB` 与手动映射；`/v1/models` 不含别名。
- 下游网关为 customerA 的流量把 `modelA` 改名为 `modelA_customerA` 上送，日志即按客户区分。

## 8. 验证

- `gofmt` 干净；`go build ./...`（除既有 `web/classic/dist` embed 占位）、`go test ./model/ ./controller/` 通过。
- 单测：`model/channel_customer_alias_test.go`（展开/幂等/移除清理/保留手动映射/空客户 noop）、`relay/helper/price_customer_alias_test.go`（回退 4 分支）通过。
- 前端 `bun run typecheck` / `bun run build` 通过，改动文件 lint 0 error。
- 注：`relay/helper` 的 `TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns` 为既有的环境相关计时 flaky，与本功能无关。
