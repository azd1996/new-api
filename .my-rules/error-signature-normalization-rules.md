# 错误归一化规则（error signature normalization）

> 记录我自己为 `service/error_signature.go` 的 `NormalizeErrorSignature` 追加/维护的归一化规则。
> 机制背景：`NormalizeErrorSignature` 按顺序对原始错误消息应用一组正则 `Pattern -> Replacement`（Go RE2），
> 结果就是错误统计（error-stats）里用来分组/去重的 "signature"。规则本身是 `SignatureRule{Name, Pattern, Replacement, Enabled}`，
> 管理员可在后台编辑并持久化到 `ErrorSignatureRules` Option，多实例间由 `SyncErrorSignatureRules` 轮询同步。
> 默认内置规则见 `DefaultErrorSignatureRules`（折叠空白、去掉 new-api 自身的 `(request id: ...)`、掩码 UUID/十六进制/各类 request-id/时间戳等）。

---

## 规则列表

### 1. AWS Bedrock `InvokeModelWithResponseStream` 包装 → 裁剪出 thinking signature 校验失败的核心消息

**问题**

Bedrock 的 `InvokeModelWithResponseStream` 报错会在真正的 `ValidationException` 消息外面套一层 SDK 操作错误信息，形如：

```
InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: ***.***.content.*: Invalid `signature` in `thinking` block
```

其中：
- 前缀（`InvokeModelWithResponseStream: operation error ... RequestID: ... ValidationException: ***.***.content.*:`）每次请求都不同（RequestID、路径索引等），不能作为稳定 signature 的一部分；
- 末尾的 `` Invalid `signature` in `thinking` block `` 后面可能什么都没有，也可能还跟着别的字符串（不同 SDK/网关版本尾部内容不固定）。

真正有业务含义、应该用来分组统计的，只有核心一句：

```
Invalid `signature` in `thinking` block
```

这和 `.my-rules/rules-catalog.md` 里 R6 重试规则命中的错误是同一类问题（Claude 经 AWS Bedrock 兜底时 thinking 块 signature 校验不过），这里是归一化侧的对应处理。

**规则（`SignatureRule`）**

```json
{
  "name": "bedrock-thinking-signature-invalid",
  "pattern": "(?s).*Invalid `signature` in `thinking` block.*",
  "replacement": "Invalid `signature` in `thinking` block",
  "enabled": true
}
```

- `pattern` 用 `(?s)` 让 `.` 也能匹配换行，前后各一个贪婪 `.*` 把整条消息（不管前面是什么包装、后面有没有尾随内容）整体吃掉；
- 由于目标短语在消息里只会出现一次，贪婪匹配最终落点和非贪婪一致，直接整串替换成固定文案即可，不需要捕获组。
- 放在规则列表里的位置不敏感（不依赖其它规则先跑），但建议放在 `collapse-whitespace` 之后，避免把文案里的空白意外影响命中。

**示例**

| 输入 | 输出 |
|---|---|
| `InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: ***.***.content.*: Invalid \`signature\` in \`thinking\` block` | `` Invalid `signature` in `thinking` block `` |
| 同上，末尾再接任意尾巴，例如 `... Invalid \`signature\` in \`thinking\` block (extra trailing text)` | `` Invalid `signature` in `thinking` block `` |

### 2. AWS Bedrock `InvokeModelWithResponseStream` 包装 → 裁剪出 web_search 工具不支持的核心消息

**问题**

同样是 Bedrock SDK 包装错误，这次是模型不支持 `web_search_20250305` 工具类型：

```
InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: tool type 'web_search_20250305' is not supported for this model
```

前缀部分（操作名、RequestID 等）每次不同；核心一句 `tool type 'web_search_20250305' is not supported for this model` 末尾（`model` 之后）也可能跟着其它字符串（不同网关/版本尾部不固定）。

真正用于分组统计的核心短语：

```
tool type 'web_search_20250305' is not supported for this model
```

**规则（`SignatureRule`）**

```json
{
  "name": "bedrock-web-search-tool-unsupported",
  "pattern": "(?s).*tool type 'web_search_20250305' is not supported for this model.*",
  "replacement": "tool type 'web_search_20250305' is not supported for this model",
  "enabled": true
}
```

- 与规则 1 同样的写法：`(?s)` 让 `.` 匹配换行，前后贪婪 `.*` 吃掉整条消息（不管前面包装、后面尾巴），整体替换成固定文案。
- 目标短语只出现一次，贪婪匹配结果等价于非贪婪，无需捕获组。

**示例**

| 输入 | 输出 |
|---|---|
| `InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: tool type 'web_search_20250305' is not supported for this model` | `tool type 'web_search_20250305' is not supported for this model` |
| 同上，末尾再接任意尾巴，例如 `... is not supported for this model (extra trailing text)` | `tool type 'web_search_20250305' is not supported for this model` |

### 3. AWS Bedrock `InvokeModel` 包装 → 裁剪出账号无权限访问的核心消息

**问题**

账号未开通 / 未被允许访问 Bedrock 模型时的报错，同样带 SDK 包装前缀，且原始消息末尾带句号：

```
InvokeModel: operation error Bedrock Runtime: InvokeModel, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: Access to Bedrock models is not allowed for this account.
```

前缀（操作名、RequestID）每次不同；核心一句后面的句号/其它尾随内容也不固定，统一裁剪掉。

真正用于分组统计的核心短语（不带句号）：

```
Access to Bedrock models is not allowed for this account
```

**规则（`SignatureRule`）**

```json
{
  "name": "bedrock-account-access-not-allowed",
  "pattern": "(?s).*Access to Bedrock models is not allowed for this account.*",
  "replacement": "Access to Bedrock models is not allowed for this account",
  "enabled": true
}
```

- 同样的写法：`(?s)` 让 `.` 匹配换行，前后贪婪 `.*` 吃掉整条消息（含末尾句号或其它尾巴），整体替换成固定文案（不含句号）。

**示例**

| 输入 | 输出 |
|---|---|
| `InvokeModel: operation error Bedrock Runtime: InvokeModel, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: Access to Bedrock models is not allowed for this account.` | `Access to Bedrock models is not allowed for this account` |
| 同上，末尾再接任意尾巴 | `Access to Bedrock models is not allowed for this account` |

### 4. AWS Bedrock `InvokeModel` 包装 → 裁剪出输入过长的核心消息

**问题**

请求输入超出模型支持长度时的报错，同样带 SDK 包装前缀，原始消息末尾带句号：

```
InvokeModel: operation error Bedrock Runtime: InvokeModel, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: Input is too long for requested model.
```

前缀（操作名、RequestID）每次不同；核心一句后面的句号/其它尾随内容也不固定，统一裁剪掉。

真正用于分组统计的核心短语（不带句号）：

```
Input is too long for requested model
```

**规则（`SignatureRule`）**

```json
{
  "name": "bedrock-input-too-long",
  "pattern": "(?s).*Input is too long for requested model.*",
  "replacement": "Input is too long for requested model",
  "enabled": true
}
```

- 同样的写法：`(?s)` 让 `.` 匹配换行，前后贪婪 `.*` 吃掉整条消息（含末尾句号或其它尾巴），整体替换成固定文案（不含句号）。

**示例**

| 输入 | 输出 |
|---|---|
| `InvokeModel: operation error Bedrock Runtime: InvokeModel, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: Input is too long for requested model.` | `Input is too long for requested model` |
| 同上，末尾再接任意尾巴 | `Input is too long for requested model` |

### 5. AWS Bedrock `InvokeModelWithResponseStream` 包装 → 裁剪出 assistant 消息 prefill 不支持的核心消息

**问题**

`claude-opus-5-5` 不支持 assistant message prefill（即最后一条消息是 `role=assistant` 用来引导续写）的报错，同样带 SDK 包装前缀，原始消息末尾带句号：

```
InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: This model does not support assistant message prefill. The conversation must end with a user message.
```

前缀（操作名、RequestID）每次不同；核心两句后面的句号/其它尾随内容也不固定，统一裁剪掉。

真正用于分组统计的核心短语（不带末尾句号）：

```
This model does not support assistant message prefill. The conversation must end with a user message
```

**规则（`SignatureRule`）**

```json
{
  "name": "bedrock-assistant-prefill-unsupported",
  "pattern": "(?s).*This model does not support assistant message prefill\\. The conversation must end with a user message.*",
  "replacement": "This model does not support assistant message prefill. The conversation must end with a user message",
  "enabled": true
}
```

- 同样的写法：`(?s)` 让 `.` 匹配换行，前后贪婪 `.*` 吃掉整条消息（含末尾句号或其它尾巴），整体替换成固定文案（不含末尾句号）。
- 中间句号 `This model does not support assistant message prefill.` 这个句号是字面量，需要转义成 `\.`，避免被正则解释成任意字符（虽然这里恰好也能命中，但转义更准确、也更稳妥）。

**示例**

| 输入 | 输出 |
|---|---|
| `InvokeModelWithResponseStream: operation error Bedrock Runtime: InvokeModelWithResponseStream, https response error StatusCode: 400, RequestID: ********-****-****-****-************, ValidationException: This model does not support assistant message prefill. The conversation must end with a user message.` | `This model does not support assistant message prefill. The conversation must end with a user message` |
| 同上，末尾再接任意尾巴 | `This model does not support assistant message prefill. The conversation must end with a user message` |

**为什么参数覆盖规则（param_override）处理不了这个问题，以及怎么处理**

详见下方「与参数覆盖规则的关系」一节，这里只记归一化规则本身。
