# 覆盖规则命名模板（Override Rule Templates）

> 作用：把参数覆盖（param_override）和重试覆盖（retry_override）规则抽成「命名模板」集中维护，渠道按名字多选引用；修改模板后，所有引用它的渠道**运行时立即生效**，无需逐个渠道复制粘贴/重存。

## 背景与动机

随着渠道增多，每个渠道都要手配一长串 param_override / retry_override 规则；规则更新后还要在各渠道间复制粘贴。该功能提供一个共享模板库：

- 在系统设置里集中编辑「命名规则集」（例如 `claude-paramoverride`，一个名字对应**一个规则列表**）。
- 在每个渠道的「参数覆盖」「重试覆盖规则」处，用多选框按名字勾选要引用的模板。
- 请求处理时，把引用的模板规则 + 渠道自配规则合并成完整规则列表后生效。

## 关键设计决策

- **存储：operation_setting JSON blob，不建新表。** 复用现有「系统设置选项」机制，热更新、零迁移。options 表 `value` 列为 GORM `string` → MySQL `LONGTEXT`（PG/SQLite 为 `text`），容量无忧。代价：整块读写、后写覆盖，适用于「模板低频、基本单人编辑」。
- **生效方式：运行时解析。** 渠道只存引用名；请求时按名解析模板并合并。改模板 → 所有引用渠道立即生效，无需重存渠道。
- **合并顺序：模板在前、渠道自配在后。** 与现有「渠道亲和性」的合并语义一致（模板 operations 前置，渠道自己的规则可在共享模板之上做最后微调）。
- **与渠道亲和性 param 模板共存时：亲和性在最外层。** 执行顺序 = 亲和性模板 → 引用模板 → 渠道自配（改动最小，保亲和性原行为不变）。
- **严格区分类型。** 模板分 param / retry 两类；渠道两个多选框各只列对应类型，避免混用。
- **缺失/非法引用名：静默跳过。** 与现有「规则解析失败则跳过、不致命」一致。

## 数据模型与存储

模板库注册为设置 `override_template_setting`（`setting/operation_setting/override_template_setting.go`），落到两个 option 键：

- `override_template_setting.param_templates`
- `override_template_setting.retry_templates`

两个键的值都是一个 JSON 数组字符串，元素结构：

```jsonc
{
  "name": "claude-paramoverride",   // 唯一简记名，渠道按此引用
  "description": "可选说明",
  "content": <规则内容>              // param: {"operations":[...]} 对象；retry: [RetryRule, ...] 数组
}
```

- **param 模板**的 `content` 是参数覆盖对象 `{"operations":[...]}`（与渠道「参数覆盖」编辑器所编辑的 JSON 同形；也兼容直接给 operations 数组，内部会包一层）。
- **retry 模板**的 `content` 是重试规则数组（与渠道 `retry_override` 同形）。

渠道侧新增两个引用字段，存在渠道自身的 setting blob（`dto.ChannelSettings`）里：

- `param_override_template_refs: []string`
- `retry_override_template_refs: []string`

## 运行时合并

- **参数覆盖**：`middleware/distributor.go` 取 `channel.GetParamOverride()` 后，先用 `service.MergeParamOverrideTemplates(refs, base)` 把引用模板按选择顺序前置合并（复用 `service.mergeChannelOverride`，从后往前折叠以保证「模板按选择顺序在前、渠道自配在后」）；随后现有「渠道亲和性」合并仍在最外层。最终执行顺序：**亲和性 → 引用模板 → 渠道自配**。
- **重试覆盖**：`controller/retry_fallback.go` 用 `service.ResolveRetryOverrideTemplates(refs)` 取到模板规则，按选择顺序 **prepend** 到渠道自配的 `RetryOverride` 之前（`ops = 模板规则... + 渠道自配...`）。兼容 relay 循环传入的 stub 渠道（Setting 为空时回读完整渠道）。
- 解析逻辑集中在 `service/override_template.go`；缺失/解析失败的引用静默跳过。

## 校验

保存模板库时（`controller/option.go` 的 `validateOverrideTemplatesJSON`）：必须是 JSON 数组；每个元素名字非空且**同类型内唯一**；必须有 `content` 字段。`content` 的语义合法性在请求时宽松处理（坏条目跳过，不拖垮整个渠道）。

## 接口

- 模板库的增删改查：复用 **root 权限**的通用选项接口 `GET/PUT /api/option`（即系统设置页用的那套）。
- 渠道编辑页取模板名：`GET /api/channel/override_templates`（**AdminAuth**，`controller/override_template.go`），返回 `{ data: { param: [{name,description}], retry: [{name,description}] } }`。只暴露名字/描述，不暴露规则内容。

> 权限边界：编辑模板需 root；渠道管理员只能**读取模板名并引用**，不能改模板内容。

## 前端

- 管理页：系统设置 → Models 页内「Override Templates」一节（`web/default/src/features/system-settings/general/override-templates/`，仿 channel-affinity；param 复用 `ParamOverrideEditorDialog` 可视化编辑器，retry 用 JSON 文本框）。
- 渠道编辑：抽屉里「参数覆盖」「重试覆盖规则」旁各一个 `MultiSelect`（按名多选，选项来自上面的接口，支持 allowCreate）；引用名经 `channels/lib/channel-form.ts` 读写进渠道 setting JSON。

## 相关变更：移除「200 限流强制 fallback」

本次一并移除了 retry-override 里的一个特殊处理：旧逻辑在上游 `status_code == 200`（body 内夹带限流等错误）命中规则时，会把动作**强制**改成 `fallback_next_channel`。现已去掉——200 命中也遵循规则自身的 `phase4_retry_action`（默认 `retry_same_channel`）。改动位于 `relay/retryrule/retryrule.go` 的 `CollectRewrites`，相关测试、注释与前端/ i18n 文案同步更新。

## 使用示例

### 1. 在模板库里建一个 param 模板

系统设置 → Models → Override Templates → Parameter Override Templates → 新增：

- Name：`claude-paramoverride`
- Content（`{"operations":[...]}` 对象）：

```json
{
  "operations": [
    {
      "description": "claude 5 系列删除已弃用的 temperature",
      "mode": "delete",
      "path": "temperature",
      "conditions": [
        { "path": "model", "mode": "contains", "value": "claude-opus-5" },
        { "path": "model", "mode": "contains", "value": "claude-sonnet-5" }
      ],
      "logic": "OR"
    },
    {
      "description": "删除 messages 里的 system 角色",
      "mode": "prune_objects",
      "path": "messages",
      "value": { "where": { "role": "system" }, "recursive": false }
    }
  ]
}
```

存储后，`override_template_setting.param_templates` 的值形如：

```json
[{"name":"claude-paramoverride","description":"","content":{"operations":[/* 同上 */]}}]
```

### 2. 建一个 retry 模板

Retry Override Templates → 新增：

- Name：`claude-retry`
- Content（`[RetryRule, ...]` 数组）：

```json
[
  {
    "description": "thinking 签名校验失败时去掉 thinking 块后重试当前渠道",
    "phase2_response_conditions": {
      "conditions": [
        { "path": "status_code", "mode": "full", "value": 400 },
        { "path": "error_message", "mode": "contains", "value": "Invalid 'signature' in 'thinking' block" }
      ],
      "logic": "AND"
    },
    "phase3_request_rewrite": [
      { "mode": "prune_objects", "path": "messages", "value": { "where": { "type": "thinking" } } }
    ],
    "phase4_retry_action": "retry_same_channel"
  }
]
```

### 3. 渠道引用

编辑任意渠道 → 「参数覆盖」旁的多选框勾选 `claude-paramoverride`，「重试覆盖规则」旁的多选框勾选 `claude-retry`。保存后渠道 setting 里会写入：

```json
{
  "param_override_template_refs": ["claude-paramoverride"],
  "retry_override_template_refs": ["claude-retry"]
}
```

### 4. 请求时的等效效果

- **参数覆盖**最终执行序：`渠道亲和性模板 → claude-paramoverride 的 operations → 渠道自己「参数覆盖」里的 operations`。
- **重试覆盖**最终规则列表：`claude-retry 的规则... + 渠道自己 retry_override 的规则...`。

之后只要改 `claude-paramoverride` / `claude-retry` 模板本身，**所有勾选了它的渠道**下一次请求即生效，无需逐个渠道改动。多选多个模板时，按勾选顺序拼接。

## 涉及文件速查

- 后端：`setting/operation_setting/override_template_setting.go`、`service/override_template.go`(+`_test.go`)、`dto/channel_settings.go`、`middleware/distributor.go`、`controller/retry_fallback.go`、`controller/option.go`、`controller/override_template.go`、`router/channel-router.go`、`relay/retryrule/retryrule.go`(+`_test.go`)
- 前端：`web/default/src/features/system-settings/general/override-templates/*`、`.../system-settings/models/section-registry.tsx`、`.../models/index.tsx`、`.../system-settings/types.ts`、`web/default/src/features/channels/{api.ts,lib/channel-form.ts,components/drawers/channel-mutate-drawer.tsx}`、`web/default/src/i18n/locales/*.json`

