# 客户端错误响应脱敏方案

状态：方案待实施。本文记录已确认的需求与实施约束，不代表功能已经上线。

## 1. 目标与已确认决策

当错误消息包含敏感片段时，只替换客户端看到的该片段，其余错误内容保留。例如：

```text
内部记录：No available channel under group bct-aws_big (distributor), model example-model
客户端：  No available channel under group * (distributor), model example-model
```

- 全局配置、有序规则、支持正则捕获组替换；具体组名不得硬编码。
- 复用现有错误签名归一化的正则编译和顺序替换逻辑。
- 脱敏规则与错误签名规则分别存储、分别启用，互不影响。
- 配置页面位于「系统设置 → 请求策略」的最后一个分区，名称为「错误响应脱敏」。
- 仅在向 AI 客户端输出错误时应用；日志、错误签名、重试匹配与渠道健康判断继续使用应用本功能前的内容。
- 空规则默认保持当前行为；示例规则由管理员主动添加，不默认开启。

“内部记录不脱敏”指本功能不修改内部记录中的敏感片段。现有上游 URL 脱敏、日志长度限制等策略保留，不将现有日志改成无条件保存完整上游响应体。

## 2. 现有实现与缺口

| 现有能力 | 位点 | 与本需求的关系 |
| --- | --- | --- |
| 有序正则替换及编译缓存 | `service/error_signature.go` | 可抽取复用的底层能力 |
| 错误签名规则编辑器 | `web/src/features/error-stats/components/error-signature-rules-dialog.tsx` | 优先提取复用规则编辑部分 |
| 请求策略分区注册 | `web/src/features/system-settings/request-policies/section-registry.tsx` | 当前为 filtering、routing、health；新增分区追加在 health 之后 |
| 渠道成功响应字段过滤 | `relay/common/response_filter.go` | 删除 JSON 字段，不处理错误字符串，维持原职责 |
| 重试覆盖 return_error | `controller/relay.go`、`controller/retry_fallback.go` | 返回固定消息并终止重试，不提供片段替换 |
| 客户端错误转换 | `relaykit/types/error.go` | 已有上游 URL 脱敏，但无自定义文本规则 |

`NewAPIError.SetMessage` 当前仅修改 `Err`，部分 `ToOpenAIError` / `ToClaudeError` 分支取用 `RelayError` 内的消息。因此不能通过修改 `Err` 实现本功能，也不能假定所有响应消息等于 `Error()`。应先完成协议错误转换，再处理实际输出副本的消息字段。

重试覆盖固定消息输出不完整的问题属于独立缺陷；本方案不通过全局修改 `SetMessage` 顺带改变其行为。

## 3. 规则语义

沿用归一化规则的数据形状：

```json
[
  {
    "name": "hide-distributor-group",
    "pattern": "(under group )[^\\s]+( \\(distributor\\))",
    "replacement": "${1}*${2}",
    "enabled": true
  }
]
```

- 使用 Go `regexp`（RE2）语法，替换采用 `ReplaceAllString` 语义。
- 依次执行全部启用规则，后续规则接收前一条的结果；一条规则替换全部匹配位置。
- 支持 `${1}` 等捕获组引用；字面量美元符号使用 `$$`。
- 正则大小写默认敏感，需要时由管理员在模式中使用 `(?i)`。
- 允许空替换文本以删除片段；拒绝空正则，避免无意义的零宽批量插入。
- 名称只用于识别规则，不参与匹配。
- 不自动去空格、不追加或剥离 request ID、不进行错误签名的 2000 字节截断。
- HTTP 状态码、协议类型、错误码、重试行为、响应头均保持原有语义。
- 只处理明确的错误消息字段，不对序列化后的整段 JSON 或 SSE 文本做正则替换。

若仅隐藏某一个组，可使用：

```json
{
  "name": "hide-specific-distributor-group",
  "pattern": "(under group )bct-aws_big( \\(distributor\\))",
  "replacement": "${1}*${2}",
  "enabled": true
}
```

这两个例子是可选配置，不写入业务代码的默认规则。

## 4. 复用方式与模块边界

从 `service/error_signature.go` 提取小型的有序正则规则编译与应用能力，让错误签名归一化和客户端脱敏共用。共享规则结构可以保留现有 `SignatureRule` 的类型别名以兼容调用方。

共享部分负责规则编译、跳过禁用项、捕获组替换及按顺序执行；两种业务分别持有配置快照和编译结果。错误签名现有默认规则、空白修整、长度截断及哈希生成继续留在错误签名逻辑中，回归结果必须不变。

客户端脱敏执行器接收字符串并返回新字符串，不写入传入的错误对象。请求路径只读取已编译的不可变快照，不读数据库、不逐请求编译正则、不逐请求解析配置。

宿主负责配置存取和调用。`relaykit` 不导入宿主 `service` / `model`，不引入全局配置回调。宿主在 `ToOpenAIError` / `ToClaudeError` 等转换得到响应副本后，再调用脱敏执行器，避免把客户端展示规则带入内部日志路径。

## 5. 配置存储、更新与多实例

建议独立 Option 键 `ClientErrorRedactionRules`，值为 JSON 规则数组。沿用现有 Option 持久化机制，不新增业务表或数据库字段。

管理 API 建议提供：

- `GET /api/option/client_error_redaction`：读取当前规则。
- `PUT /api/option/client_error_redaction`：整组保存规则。
- `POST /api/option/client_error_redaction/preview`：使用未保存的规则和示例文本预览，不落库、不修改运行时配置。

端点沿用系统配置的现有权限要求，服务端校验权限与配置，不能只依赖前端。新增路由时确认静态路径与现有 option 路由的匹配顺序。

保存流程：解析并完整校验 → 编译全部启用规则 → 持久化成功 → 原子替换当前编译快照。非法 JSON、非法正则或存储失败均不得替换当前生效规则。禁用规则可保留未完成的模式，启用时必须编译通过；所有规则均拒绝空模式。

启动时读取配置；复用现有配置同步周期进行跨实例变更检查，仅配置变化时重编译。缺失配置、空值及 `[]` 均明确表示无规则，清空配置必须清除旧缓存。后台同步遇到非法配置保留上一份有效快照并记录运维错误；启动时无有效快照且持久配置非法时使用空规则并明确记录错误，不声称此时已启用脱敏。

保存实例成功后立即生效；其他实例在下一次同步后生效。`SYNC_FREQUENCY` 关闭时遵循现有配置同步约束，页面提示需要逐实例重载或重启，不承诺集群即时一致。

预览使用同一套 Go 编译与替换逻辑，不使用浏览器 JavaScript 正则模拟。示例文本不持久化，也不写入请求体诊断日志。

## 6. 客户端输出范围与接入顺序

执行顺序：

```text
原始错误 → 重试/健康判断与内部记录 → 既有错误消息选择及协议转换
         → 既有 URL 脱敏 → 自定义片段替换 → JSON/SSE/WebSocket 输出
```

每个出口只应用一次规则；不能假定规则幂等。请求 ID 的既有附加方式保持不变，但自定义正则可以匹配实际消息中的任何文本。

| 输出路径 | 接入点与要求 |
| --- | --- |
| 本机 distributor 在选渠道前失败 | `middleware/utils.go` 的 `abortWithOpenAiMessage`，直接处理输出消息副本，不依赖 ChannelSettings |
| 常规 OpenAI / Claude 错误 | `controller/relay.go` 的 `writeRelayError`，处理转换后的 `message` |
| 网关统一输出的 SSE 错误 | `writeRelayStreamError`，处理错误事件消息，保持事件格式和结束行为 |
| Realtime / Responses WebSocket 错误 | `writeRelayError` 与 `relay/responses_websocket.go` 的直接错误出口，避免重复处理 |
| 系统过载等前置错误 | `middleware/performance.go` 等 AI 路由的错误出口，无渠道时也可执行 |
| Claude token count、AI 路由 404 / 未实现 | 处理各自直接构造的错误消息字段 |
| 原生任务与 Midjourney 标准错误 | 对 TaskError 的 message、副本中的 description 等明确消息字段处理，不改原对象或日志 |

实施前须按 AI 路由与 `ToOpenAIError`、`ToClaudeError`、直接 `c.JSON`、SSE、WebSocket 写入调用点完成出口清单，逐项核对是否已覆盖。不能仅修改 controller 的最终错误函数便声称覆盖全部路径。

以下范围首期明确不提供通用清洗：成功响应体、HTTP 响应头、错误 metadata / 任意嵌套字段、上游原样透传的流内错误、插件自定义错误渲染体、历史日志和管理控制台 API。对明确消息字段之外的敏感数据不承诺覆盖；其中透传流内错误和自定义插件输出须在实施收尾列出实际未覆盖入口。若后续要覆盖插件自定义契约，应单独检查 Task Plugin API 文档后设计。

## 7. 前端配置页面

在请求策略分区列表尾部追加 `error-redaction`，路径建议为 `/system-settings/request-policies/error-redaction`，保留当前默认分区 `routing`。

页面说明：“仅替换返回给 API 客户端的错误消息片段，不影响内部日志、错误统计和重试判断。”

规则列表展示名称、启用状态、匹配正则和替换文本，支持新增、编辑、删除、上移、下移及整组保存。提供独立的示例文本输入与预览结果，预览可使用未保存的规则。保存、预览错误显示具体规则与原因，失败时保留当前编辑内容。

优先从 `error-signature-rules-dialog.tsx` 提取可复用的规则编辑内容，通过 props 提供规则与变更回调；原错误统计弹窗继续使用原 API、原文案和原行为。页面使用系统设置的现有布局，删除确认、加载/错误状态等复用共享业务组件，不复制整份编辑器。

新增预览区域作为本功能的业务组合，不把错误签名 API 写入共享编辑组件。新增文案仅提供 en / zh，其余语言沿用英文回退。页面明确 RE2 语法、捕获组写法、顺序执行及首期范围，不将它描述为所有响应的全面脱敏。

## 8. 验证与验收

优先扩展现有适合的测试文件；后端本功能如确需新测试文件，最多新增一个，集中保护关键契约。

- 规则：准确替换组名、保留前后文、重复匹配、捕获组、多规则顺序、禁用、空列表、清空配置、非法正则不覆盖有效快照。
- 复用回归：原错误签名默认规则输出、哈希及长度处理均不变。
- 客户端与日志分离：使用结构化 OpenAI / Claude 原始错误，验证实际响应消息已替换，内部 `Err`、`RelayError` 和错误日志仍保留该组名。
- 出口：覆盖 distributor 尚未选到渠道、非流式、SSE 和 WebSocket 的关键输出，断言状态码、code、type 与流终止语义保持不变。
- 配置：验证保存失败保持旧规则、启动加载、跨实例刷新和清空规则；预览不修改生效快照。
- 前端：验证规则顺序/启停编辑、保存失败保留输入、预览使用未保存内容，以及旧错误签名编辑功能可继续使用。

实施后运行受影响 Go 测试与 gofmt；若修改 relaykit，必须在该模块运行 `GOWORK=off go build ./...`。前端执行受影响测试、类型检查、涉及文件 lint 及生产构建。

若持久化实现涉及数据库行为，按仓库要求使用真实 SQLite、MySQL、PostgreSQL 验证并记录版本、命令及结果；未完成时明确列为阻塞，不以“没有新表”免除验证。

最终验收以示例错误在客户端显示 `under group * (distributor)`、内部记录仍含原组名为核心，同时确认错误签名统计不受规则编辑影响。回滚方式为保存空规则列表，并确认各实例已同步。
