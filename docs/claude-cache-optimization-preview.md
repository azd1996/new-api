# Claude 缓存优化测试与全链路预览

客户编辑抽屉中的「缓存优化测试」区域提供两种不回源的请求预览方式，用于验证客户级 Claude 缓存命中率优化配置的实际效果。

- 模式一：**仅缓存优化**（`cache_optimization`）——直接对请求体套用缓存优化管道，用于快速验证规则改写。
- 模式二：**全链路**（`full_relay`）——尽量复现生产转发链路（解析、渠道选择、模型映射、协议转换、覆盖规则、缓存优化），捕获最终出站 HTTP 请求但不发送上游。

两种模式都不会真实请求上游，也不会产生计费、日志或渠道状态变更（多 Key 轮询渠道的一个已知例外见下文「生产影响分析」）。

## 1. 范围

### 全链路模式包含

- Claude `/v1/messages` 请求解析与校验
- 按分组选择渠道（与生产同一套渠道缓存 / 数据库逻辑）
- 渠道上下文初始化：模型映射、参数覆盖、请求头覆盖、系统提示词
- `metadata.user_id` 注入
- 协议转换（含格式互转）
- 禁用字段移除
- 缓存优化规则（fallback、fix volatile、stable base、clear_at 清理、强制 1h TTL）
- 出站 HTTP 请求构建（URL、方法、Header、Body）

### 不包含

- 真实网络请求（捕获点在 `doRequest` 之前，不建立连接）
- 计费、预扣费、结算、消耗/错误日志落库
- 重试、渠道错误处理、自动禁用
- 响应处理（不执行 `DoResponse`）
- API token 鉴权 / 限流（接口本身受管理员鉴权保护）
- 保留上一断点规则（依赖上游缓存状态，预览中标记为 `skipped_steps`）

## 2. 架构与数据流

```
前端(测试区) ──POST /api/customer/cache_optimization/test──▶ Controller
   mode=cache_optimization                      mode=full_relay
        │                                             │
        ▼                                             ▼
 relay.PreviewClaudeCacheOptimization        relay.PreviewClaudeRelay
   - 解析 model 名                              - helper.GetAndValidateClaudeRequest
   - resolveClaudeCacheOptimization            - GenRelayInfoClaude（分组/模型上下文）
   - 生产缓存优化函数                            - CacheGetRandomSatisfiedChannel（渠道选择）
     fallback/fix/stable/clear_at/1h           - SetupContextForSelectedChannel（映射/覆盖）
        │                                      - ClaudeHelper（完整转换+缓存优化）
        ▼                                             │
   返回 body/规则/步骤                        RelayInfo.DryRunCapture 捕获出站请求
                                                     ▼
                                        channel.DoApiRequest 在 doRequest 前返回
                                        合成响应（不建连、不发送）
```

关键机制：

- `RelayInfo.DryRunCapture *UpstreamRequestCapture`（`relay/common/relay_info.go`）：非 nil 时启用捕获。
- `relay/channel/api_request.go`：`DoApiRequest` / `DoFormRequest` 在请求完全构建后，若 `DryRunCapture != nil`，记录 Method/URL/Headers/Body 并返回合成 200 响应，不进入 `doRequest`。
- `ClaudeHelper` 在 `DoRequest` 返回后检测 dry-run，直接 return，不调 `DoResponse`、不结算、不写缓存骨架。
- 预览配置通过 `ContextKeyClaudeCacheOptimizationPreviewOverride` 注入，使全链路模式也能使用表单中未保存的缓存配置。

## 3. API 契约

```jsonc
POST /api/customer/cache_optimization/test
{
  "customer_id": "xxx",          // 可选；无 config 时读取该客户已保存配置
  "config": { ... },             // 可选；表单中的缓存配置，优先级最高
  "group": "default",            // 分组，默认 default
  "mode": "cache_optimization" | "full_relay",  // 默认 cache_optimization
  "items": [
    { "body": "...", "headers": { ... } }       // headers 仅全链路模式用于模拟请求头
  ]
}
```

响应 `results[]` 公共字段：

| 字段 | 说明 |
|---|---|
| `index` / `ok` | 序号与是否成功 |
| `body` | 处理后的请求体 |
| `model` | 请求体中的模型名 |
| `error` | 失败原因 |
| `enabled_rules` | 该请求命中的缓存优化规则 |
| `changed_steps` | 实际产生改写的规则/步骤（仅缓存优化模式） |
| `skipped_steps` | 无法在预览中执行的规则（保留上一断点） |
| `headers` | 缓存优化需要附加的请求头（如 `anthropic-beta`） |

`full_relay` 额外字段：

| 字段 | 说明 |
|---|---|
| `channel_id` / `channel_name` / `channel_type` | 本次选中的渠道 |
| `group` | 实际使用的分组 |
| `origin_model` / `upstream_model` / `model_mapped` | 模型映射结果 |
| `pass_through` | 是否处于透传模式（此时缓存优化被跳过） |
| `url` / `method` | 出站请求 URL（敏感 query 已脱敏）与方法 |
| `headers` | 出站请求头（敏感值已脱敏） |

限制：每项请求体 ≤ 2MB，最多 20 项；仅支持 Claude 请求格式；`mode` 非法时返回错误。

## 4. 前端交互

- 测试区位于客户编辑抽屉「Claude cache optimization」配置块上方。
- 「测试模式」下拉：仅缓存优化 / 全链路（不回源）。
- 分组输入默认 `default`；全链路模式使用该分组选择渠道。
- 全链路结果额外展示渠道、分组、模型映射（如 `claude-sonnet-4 → claude-3-5-sonnet`）、上游 URL/方法、透传提示。
- 两种模式均使用**当前表单中的缓存配置**（未保存也可测试）；未自定义时回退到客户已保存配置或全局配置。

## 5. 安全

- 敏感响应头（`authorization`、`x-api-key`、`*token*`、`*secret*`、`*cookie*` 等）一律脱敏为 `***`。
- URL 中的敏感 query 参数通过 `SanitizeURLForLog` 脱敏。
- 接口挂在 customer 路由组下，受 `AdminAuth` 保护。
- 不发送上游，不会将请求内容泄露给上游。

## 6. 生产影响分析

### 6.1 正常转发热路径（无预览时）

| 改动点 | 生产行为 | 开销 |
|---|---|---|
| `RelayInfo` 新增 `DryRunCapture` 指针字段 | 恒为 nil，零行为差异 | 每请求 +8 字节 |
| `DoApiRequest` / `DoFormRequest` 增加 nil 检查 | 恒为 false，直接跳过 | 1 次指针比较 |
| `ClaudeHelper` 经 `claudeCacheOptimizationForRequest` 取配置 | 额外 1 次 gin context map 查找（覆盖 key 生产中不存在），结果与原逻辑一致 | 每请求约 2 次 map 查找 |
| `finishCacheSkeleton` / defer 中的 nil 检查 | 恒为 false | 1 次指针比较 |

结论：**生产转发功能上完全无损**，无新增分配，仅可忽略的判断开销。

### 6.2 预览执行副作用

已确认不会发生：

- 不建立 TCP/TLS 连接，不解析代理，不做被动代理健康采样
- 不触发渠道错误处理、自动禁用、重试计数
- 不预扣/结算配额，不写消耗/错误日志
- 不写缓存骨架、不记忆断点状态
- 渠道选择读取共享缓存时只持读锁

### 6.3 已知并接受的例外：多 Key 轮询渠道

`SetupContextForSelectedChannel` → `Channel.GetNextEnabledKey()` 在 **多 Key 轮询模式**（`MultiKeyModePolling`）下会：

1. 在内存缓存模式下修改共享渠道对象（`channelsIDM` 中的指针）的 `ChannelInfo.MultiKeyPollingIndex`；
2. 在内存缓存关闭时调用 `SaveChannelInfo()` 写数据库。

即：运行一次全链路预览会推进该渠道的 key 轮询索引，使下一个真实请求可能使用与原本不同的 key。这不会造成错误，但严格来说改变了线上请求的 key 分配顺序。

**决策记录（2026-11-23）：该副作用已被明确评估并接受，不做修复。** 如未来要求严格无损，可在预览路径使用渠道副本 + 只读 peek 选 key（避免推进轮询索引与 DB 写入）。

### 6.4 预览与真实请求的固有差异（保真度说明）

- 同分组多渠道时，生产按权重随机选择；预览也只反映"本次抽样"的渠道，重复运行可能得到不同渠道。
- 会话路由（`behind-` 分组）依赖会话上下文；预览构造的是无会话状态的新请求，可能落入标准池。
- 不模拟 API token 鉴权/限流。
- 多 Key 渠道预览展示的 key 与真实请求可能不同（轮询索引被预览推进）。

## 7. 实现位置

| 模块 | 文件 |
|---|---|
| 请求解析/类型/路由 | `controller/customer.go`、`router/api-router.go` |
| 缓存优化预览 | `relay/claude_cache_optimization_preview.go` |
| 全链路预览 | `relay/claude_relay_preview.go` |
| 请求捕获与敏感头脱敏 | `relay/channel/api_request.go` |
| `RelayInfo` 扩展 | `relay/common/relay_info.go` |
| dry-run 保护 | `relay/claude_handler.go`、`relay/cache_skeleton.go` |
| 预览配置覆盖 key | `constant/context_key.go` |
| 前端测试区 | `web/src/features/customers/components/customer-cache-policy-test.tsx` |
| 前端 API | `web/src/features/customers/api.ts` |

## 8. 测试

- `relay/claude_handler_cache_ttl_test.go`：缓存优化管道预览测试。
- `relay/claude_relay_preview_test.go`：注入渠道跑完整管道，断言 URL、body 缓存改写、TTL、敏感头脱敏、规则列表。
- `controller/customer_test.go`：端点契约（缓存模式）与非法 mode 校验。
- `web/src/features/customers/components/__tests__/cache-policy-test.test.tsx`：两种模式的前端交互与渲染。

## 9. 待办 / 未决

- 全链路目前仅支持 Claude 请求格式；如需支持 OpenAI 等其他格式，需扩展 `PreviewClaudeRelay` 之外的预览入口。
- 多 Key 轮询副作用按 6.3 的决策保持现状。
