# 响应过滤规则（Response Filter）方案文档

> 渠道级能力：在上游**成功响应**返回给客户端前，按配置从响应体 JSON 中删除指定字段。通用、与具体字段无关，可配置任意路径/键名。
>
> 起因：客户端经本网关访问 Azure gpt 模型，触发内容过滤器后，Azure 在**成功响应体**里回传详细的 content_filter 结构（chat 的 per-choice `content_filter_results` + 顶层 `prompt_filter_results`；Responses API 的 `content_filters` 数组）。网关原本对成功响应体是原样透传（`IOCopyBytesGracefully`），这些细节会直接暴露给客户。本功能提供一个通用的"响应体字段清洗"开关来屏蔽它们。

---

## 1. 能力边界

- **只作用于成功响应体**（流式 + 非流式）。错误响应走 `types.OpenAIError` 重建，本就只保留 `message/type/param/code`，不含 `innererror`/`content_filter_result`，无需再处理。
- **只接入 OpenAI adaptor**（覆盖 Azure / OpenAI 的 chat completions 与 Responses API）。claude / gemini 等格式暂不接入；如将来需要，在对应 handler 的写出点调用同一函数即可。
- **零改动即零开销**：规则为空直接原样返回；非 JSON body（如流式 `[DONE]` 哨兵、SSE 事件行）原样返回；任何解析/改写错误都吞掉并返回原 body——清洗绝不破坏转发。

## 2. 配置 Schema

渠道级 `setting.response_filter`，恒为字符串数组 `[]string`，每条是一条规则：

```json
{
  "response_filter": [
    "content_filter_results",
    "prompt_filter_results",
    "content_filters"
  ]
}
```

规则两种语义（自动区分）：

- **纯键名**（不含 `.` 或 `#`）：递归删除该键在**所有嵌套层级**的出现。最省心，Azure content_filter 分散在多层/数组内，一条键名即可全删。
- **精确路径**（含 `.` 或 `#`）：按 gjson/sjson 路径删除单个节点，例如 `choices.0.content_filter_results`。精确、互不影响同名兄弟节点。

空数组 = 关闭；空白行 / 空串自动忽略。

## 3. 核心清洗函数

`relay/common/response_filter.go`：

```go
func FilterResponseJSON(body []byte, rules []string) []byte
```

逻辑：
1. `len(body)==0` 或 `len(rules)==0` → 原样返回。
2. `!gjson.ValidBytes(body)` → 原样返回（保护非 JSON 流式帧）。
3. 遍历规则：含 `.`/`#` 的走 `sjson.DeleteBytes(body, rule)`（失败保持原 body）；纯键名收集进集合。
4. 若有纯键名规则：`common.Unmarshal` 成 `interface{}` → `deleteJSONKeysRecursive` 递归删键（map 原地删，slice 逐元素递归）→ `common.Marshal` 回写（失败保持原 body）。

SSE 每个 `data:` 行本身就是一个完整 JSON 对象，content_filter 不会被拆到多个 chunk，因此流式按 chunk 调用同一函数即可，**无需跨 chunk 缓冲**。

## 4. 接入位点（均在 `relay/channel/openai/`）

规则统一取自 `info.ChannelSetting.ResponseFilter`：

- 非流式 chat：`relay-openai.go` `OpenaiHandler`，`IOCopyBytesGracefully` 前对 `responseBody` 过滤。
- 非流式 Responses：`relay_responses.go` `OaiResponsesHandler`，同上。
- 流式 chat：`helper.go` `HandleStreamFormat` 入口对 `data` 过滤（覆盖透传 / forceFormat / thinkingToContent 所有分支）。
- 流式 Responses：`relay_responses.go` `OaiResponsesStreamHandler` 的 `process` 闭包，在 `sendResponsesStreamData` 前对 `data` 过滤（事件类型来自已解析的 `streamResponse`，用量统计不受影响）。

## 5. 数据流与持久化

- DTO 字段：`dto.ChannelSettings.ResponseFilter []string`（`json:"response_filter,omitempty"`），与 `retry_override` 同存于 channel 的 setting blob，运行时随渠道配置加载到 `RelayInfo.ChannelSetting`。
- 无新建表、无 DB 迁移。

## 6. 前端（仅 `web/default`，classic 不涉及）

- `features/channels/lib/channel-form.ts`：schema 新增 `response_filter: z.string()`；表单内用**多行文本（一行一条）**；回填时 `string[].join('\n')`，保存时按行 `trim` 去空回 `string[]` 写入 `settingObj.response_filter`。
- `features/channels/components/drawers/channel-mutate-drawer.tsx`：高级设置区新增多行 `Textarea`（label「响应过滤规则」）；纳入 `SENSITIVE_FORM_FIELDS` 与「已配置」判断（`hasAdvancedSettingsValues` / `overrideRulesConfigured`）。
- `features/channels/types.ts`：`ChannelSettings.response_filter?: string[]`。
- i18n：7 个 locale（en/zh/zh-TW/fr/ja/ru/vi）新增 "Response Filter Rules" 与长描述文案。

## 7. 典型用法：屏蔽 Azure content_filter

在目标 Azure 渠道的「响应过滤规则」里填三行：

```
content_filter_results
prompt_filter_results
content_filters
```

覆盖 chat（per-choice `content_filter_results` + 顶层 `prompt_filter_results`）与 Responses API（`content_filters` 数组），流式/非流式一致生效。

## 8. 验证

- `gofmt` 干净；`go build ./...` 通过；`go test ./relay/common`（`response_filter_test.go`：递归删键、精确路径删、同名无关字段不误删、非 JSON / 空规则不改动）通过。
- 前端 `tsgo -b` typecheck 通过；改动文件 oxlint 0 error / 0 warning。

