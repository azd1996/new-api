# Param Override Path 选择符扩展（设计草案）

> 状态：草案，待评审（2026-10-24 记录）。背景见 `.my-rules/hard-to-handle-errors.md` 第 1、5、7 条——这些报错都卡在"当前 path 只有 `*` 通配，无法按条件/位置精确定位数组元素或其字段"。
> 引擎代码：`relay/common/override.go`（`collectWildcardPaths` / `resolveOperationPaths` / `deleteValue` / `processNegativeIndex`）。

## 背景与动机

param 覆盖的 `set`/`delete` 等按路径操作，目前路径通配**只认 `*`**（map 的所有键 / 数组的所有下标），外加负索引 `-1`。遇到下面这些需求无能为力：

- 只删 **thinking 块**上的 `cache_control`（text/tool 块上的要保留）——#5。
- 只清洗 **非最新 assistant 消息**的 thinking（最新那条一字不能改）——#7。
- 只改 `tools` 里 `type==function` 的工具（而非全部）。

## 关键约束：写操作走 sjson，必须"展开成具体索引"

`delete`/`set` 底层是 `sjson`，而 **sjson 不支持 gjson 的查询语法**。这就是引擎自己写 `collectWildcardPaths` 做 `*` 展开的原因：它把通配**展开成一串具体路径**（`messages.3.content.2`），再交给 sjson 逐个写。

> 设计铁律：任何新符号都必须能在 `collectWildcardPaths` 里**解析并展开成具体索引路径**，否则 set/delete 用不了。

## 方案

<!-- SECTIONS_PLACEHOLDER -->

### 1. 谓词选择符：复用 gjson 的 `#(...)` / `#(...)#`（最高价值，优先做）

不自创语法——`conditions` 已经在用 gjson（如 `tools.#(type=="function").type`），path 沿用同一套，心智一致：

- `#(key==value)` = 第一个匹配元素
- `#(key==value)#` = 所有匹配元素

让 `collectWildcardPaths` 遇到 `#(...)` 段时：遍历该数组、对每个元素跑 gjson 条件、保留命中的、产出其具体索引，再继续展开后续段。算子只支持 gjson 已有的 `==`/`!=`/`%`(通配)/`>`/`<` 等，不自造解析器。

落地示例：

- 只删 thinking 块的 cache_control（#5）：`delete messages.*.content.#(type=="thinking")#.cache_control`
- 只清洗 assistant 消息：`messages.#(role=="assistant")#.content...`
- 只改 function 工具：`tools.#(type=="function")#...`

### 2. 位置修饰符：`@last` / `@except_last`（解决"最新 assistant 豁免"）

谓词能选"所有 assistant"，但 #7 要的是"**除最新那条之外**的 assistant"。给展开后的集合加管道修饰：

- `messages.#(role=="assistant")#|@last` → 仅最新一条 assistant 的索引
- `messages.#(role=="assistant")#|@except_last` → 除最新外的所有 assistant

于是 #22/#25 可收窄成"只碰非最新 assistant 的 thinking"：
`delete messages.#(role=="assistant")#|@except_last.content.#(type=="thinking")#.endpoint_signature`

（`@last`/`@first` 贴着 gjson 修饰风格；`@except_last` 是需自定义的那个，命名延续 gjson。）

### 3. 操作级 `scope` 开关（针对稳定不变量，优于堆 path 符号）

path 微语法叠多了（谓词+位置+字段）会很难读/难调。对"最新 assistant 的 thinking 不许动"这类**稳定业务不变量**，更可维护的是加**操作级字段**，由代码在执行前从展开结果里剔除对应路径：

```json
{ "mode": "delete", "path": "messages.*.content.*.endpoint_signature",
  "scope": "exclude_latest_assistant_thinking" }
```

语义显式、规则可读、不易写错。适合 #7；通用筛选仍用方案 1/2。

## 实现要点与坑

- **多索引删除必须从高到低**：sjson 删数组元素会重排后续下标。展开出 `content.2`、`content.5` 后要**降序删**（先 5 再 2），否则错位。参考 `prune_objects` 现有的数组删除做法。
- **JSON 里转义引号**：`#(type==\"thinking\")`，与现有 conditions 写法一致。
- **缺字段/非数组兜底**：谓词段遇非数组或字段缺失返回空（与 `collectWildcardPaths` 现有 `default: return nil` 一致），不 panic。
- **纯函数、好测**：`collectWildcardPaths` 展开是纯函数，用 table 测试给定 messages/content 结构、断言展开出的索引列表。
- **只扩展展开器，不碰 sjson**：所有新符号在展开阶段消化掉，交给 sjson 的始终是具体索引路径。

## 建议落地顺序

1. 先做 **方案 1（谓词选择符）**——一次解锁 cache_control、角色/类型筛选等一大类，并能把现有多条"广撒网"规则收窄到精确目标。
2. **方案 3（scope 开关）** 解决 #7 的"最新 assistant 豁免"单点不变量。
3. **方案 2（位置修饰符）** 按需再上（若希望纯靠 path 表达位置而不加 scope 字段）。

## 关联

- `.my-rules/hard-to-handle-errors.md`：#1(image_url)、#5(cache_control)、#7(最新 assistant thinking) 均依赖本扩展才能在 param 层精确处理。
- `docs/override-rule-templates.md`：param 覆盖的功能架构与模板机制。
- 实现入口：`relay/common/override.go` 的 `collectWildcardPaths`（展开）、`resolveOperationPaths`（入口）、`deleteValue`/`sjson`（写）。
