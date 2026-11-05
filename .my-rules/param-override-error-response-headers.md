# 参数覆盖：拦截 Web Search 并返回多个响应头

下面给出三份可独立使用的参数覆盖配置，按渠道实际需求选择一份。它们检测的是请求 `tools` 中声明了搜索工具，不要求模型已经实际调用搜索。

配置位置：渠道的参数覆盖（`param_override`），或渠道引用的命名参数规则集。已有 `operations` 时，把选中的操作对象加入现有数组，通常放在删除或改写 `tools` 的操作之前；不要用示例整体替换已有规则。

`response_headers` 依赖本次新增的后端实现，部署包含该实现的版本后才会生效。本文是配置示例，创建文档不会自动应用到渠道。

## 示例一：通用精确匹配，返回 403 和三个响应头

适用于明确禁止以下三种工具类型的渠道：

- Claude：`web_search_20250305`
- OpenAI：`web_search`
- OpenAI 预览版：`web_search_preview`

条件为 OR，命中任一类型即拒绝。精确匹配不自动覆盖其它版本的工具类型；新增类型时需要补充条件。

```json
{
  "operations": [
    {
      "mode": "return_error",
      "conditions": [
        {
          "path": "tools.#(type==\"web_search_20250305\").type",
          "mode": "full",
          "value": "web_search_20250305"
        },
        {
          "path": "tools.#(type==\"web_search\").type",
          "mode": "full",
          "value": "web_search"
        },
        {
          "path": "tools.#(type==\"web_search_preview\").type",
          "mode": "full",
          "value": "web_search_preview"
        }
      ],
      "logic": "OR",
      "value": {
        "status_code": 403,
        "message": "Web search tools are not permitted on this channel.",
        "code": "web_search_not_allowed",
        "type": "invalid_request_error",
        "skip_retry": true,
        "response_headers": {
          "X-Web-Search-Blocked": "true",
          "X-Block-Reason": "web_search_not_allowed",
          "X-Gateway-Policy": "disable_web_search"
        }
      }
    }
  ]
}
```

命中后不请求上游，按现有错误响应流程返回 HTTP 403，并附带配置的三个响应头。`skip_retry: true` 表示停止普通中继重试；其它已有的显式重试覆盖策略仍需结合渠道配置检查。

## 示例二：纯 Bedrock 渠道按工具类型包含关系拦截，返回 421

沿用现有 [my-param-rules-bedrock-block-web-search.json](my-param-rules-bedrock-block-web-search.json) 的匹配方式和 HTTP 421，增加响应头，便于调用方识别拒绝原因。

`tools.#.type` 把所有工具的 `type` 投影成数组，`contains` 检测其中是否包含 `web_search` 文本。与示例一相比，匹配范围更宽，可涵盖带版本后缀的工具类型，也可能命中其它名称包含该文本的类型。仅挂到需要这项限制的渠道。

```json
{
  "operations": [
    {
      "mode": "return_error",
      "conditions": [
        {
          "path": "tools.#.type",
          "mode": "contains",
          "value": "web_search"
        }
      ],
      "value": {
        "status_code": 421,
        "message": "web_search tool is not supported on this Bedrock channel",
        "code": "tool_not_allowed",
        "type": "invalid_request_error",
        "skip_retry": true,
        "response_headers": {
          "X-Web-Search-Blocked": "true",
          "X-Block-Reason": "tool_not_allowed",
          "X-Gateway-Policy": "bedrock_disable_web_search"
        }
      }
    }
  ]
}
```

HTTP 421 沿用现有配置，并不表示网关一定会重试。这里显式设置了 `skip_retry: true`；外部客户端或下游网关如何处理 421，取决于其自身策略。

## 示例三：只拦截 Claude 名为 web_search 的工具

适用于 Claude Messages 请求：按工具的 `name` 匹配，不依赖 `type` 中的日期版本。

匹配范围与前两例不同：它也会拒绝用户自定义但同名的工具；不会识别只有 `type: "web_search"`、没有 `name` 的 OpenAI 内置工具。

```json
{
  "operations": [
    {
      "mode": "return_error",
      "conditions": [
        {
          "path": "tools.#(name==\"web_search\").name",
          "mode": "full",
          "value": "web_search"
        }
      ],
      "value": {
        "status_code": 403,
        "message": "The web_search tool is disabled on this channel.",
        "code": "web_search_not_allowed",
        "type": "invalid_request_error",
        "skip_retry": true,
        "response_headers": {
          "X-Web-Search-Blocked": "true",
          "X-Block-Reason": "web_search_not_allowed",
          "X-Gateway-Policy": "claude_disable_web_search"
        }
      }
    }
  ]
}
```

## 字段和执行行为

| 字段 | 含义 |
| --- | --- |
| `conditions` | 请求匹配条件；没有命中时继续后续操作 |
| `logic` | 多条件关系；示例一显式设置 `OR` |
| `status_code` | 返回给调用方的 HTTP 状态码 |
| `message` | 错误信息；最终响应可能附加请求 ID 或进行地址脱敏 |
| `code` | 错误码；OpenAI 格式中表现为 `error.code` |
| `type` | OpenAI 格式的 `error.type`；Claude 格式沿用现有转换规则，不保证与此值一致 |
| `skip_retry` | 默认 `true`；设为 `false` 只是允许进入原有重试判定，不保证一定重试 |
| `response_headers` | 客户端响应头对象，所有值必须是字符串，例如 `"60"`，不能写成数字 `60` |

命中有效的 `return_error` 后，当前参数覆盖操作序列立即停止，后续操作不再执行。响应头随该错误对象传递，只在最终返回该错误时写入。若允许重试且后续请求成功，前一次错误的这些响应头不会写入成功响应。

现有 `set_header` 操作修改的是发往上游的请求头。给调用方返回错误时，应使用这里的 `value.response_headers`。

## 响应头约束

- 名称不区分大小写，解析后规范化；不要同时配置 `X-Flag` 和 `x-flag`。
- 每个名称对应一个字符串值；当前不支持数组值、同名多行或变量插值。
- 空字符串是合法值，会设置一个空值响应头。
- 名称和值必须符合 HTTP 头格式，不能含有非法字符或注入换行。
- 以下头由响应格式或 HTTP 传输层管理，不能在这里覆盖：`Content-Length`、`Transfer-Encoding`、`Connection`、`Keep-Alive`、`Trailer`、`Upgrade`、`Proxy-Connection`、`Content-Type`、`Content-Encoding`。

如果 `response_headers` 配置非法，沿用参数覆盖引擎现有行为：记录错误日志并跳过整个操作，后续操作继续执行。因此，非法配置不会完成预期拦截，使用前应检查格式和实际响应。

## 流式请求边界

`stream: true` 本身不影响拦截；只要 HTTP 响应头尚未发出，就能返回指定状态码和多个响应头。

如果 Claude 流式心跳已经提交了响应头，后续错误只能通过当前 SSE 流返回，无法再修改 HTTP 状态码或添加这些响应头。这是已开始发送 HTTP 响应后的限制。
