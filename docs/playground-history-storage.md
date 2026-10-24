# 游乐场聊天历史的存储与多环境串台

> 适用场景：在同一台机器上先后/同时部署多套 new-api 实例时，游乐场（Playground）出现「没用过却有旧聊天记录」的现象。本文解释存储位置、根因，以及与 `SESSION_COOKIE` 隔离问题的异同。参见姊妹文档 `docs/session-cookie-isolation.md`。

## 一、存储位置：浏览器 localStorage，不落后端

游乐场聊天历史**完全存在浏览器本地的 `localStorage`**，不发送到后端持久化，也不使用 sessionStorage / IndexedDB / cookie。

存储键名（`web/default/src/features/playground/constants.ts` 的 `STORAGE_KEYS`）：

- `playground_messages` —— 聊天消息 / 对话历史
- `playground_config` —— 模型、分组、温度等配置
- `playground_parameter_enabled` —— 参数开关状态

读写与清空实现：`web/default/src/features/playground/lib/storage/storage.ts`（`localStorage.getItem/setItem/removeItem`）。

- `saveMessages` 把消息包成 `{version, data}` 信封后写入 `playground_messages`；由 `hooks/use-playground-state.ts` 带 500ms 防抖触发，卸载时 flush。
- `loadMessages` 挂载时从 localStorage 恢复并做规范化/裁剪。
- `clearPlaygroundData` 依次删除上述三个 key（游乐场底部的清空按钮）。

容量约束（`lib/storage/storage-schema.ts`）：最多 100 条消息、1MB 上限，超限直接丢弃该 key。

## 二、根因：origin 相同就共享，浏览器不认「哪套部署」

`localStorage` 的作用域是完整 **origin（协议 + 域名 + 端口）**。浏览器只认 origin，不认背后是哪套 new-api 实例。

因此「没用过却有旧历史」的真正原因是：**新环境复用了旧（已删）环境用过的同一个 `host:port`**。origin 一模一样 → 浏览器视为同一站点 → 直接读到旧环境遗留在本地的 `playground_messages`。删掉旧后端服务并不会清理浏览器里的这份数据。

验证方法：DevTools → Application → Local Storage → 当前域名下查看 `playground_messages` 原始 JSON。

## 三、与 SESSION_COOKIE 隔离问题的异同

同属「浏览器按作用域存储、不区分部署」这一类问题，但作用域粒度不同：

- **Cookie（SESSION_COOKIE）**：作用域 `host + path`，**不区分端口**（RFC 6265）。`localhost:3000` 与 `:3001` 共享 cookie → 登录态互相覆盖 → 必须显式隔离（`SESSION_COOKIE_NAME`）。
- **localStorage（游乐场历史）**：作用域包含端口，**不同端口天然隔离**。只有在 origin 完全相同时才会串。

即：cookie 默认会串、得动手隔离；localStorage 默认已隔离、仅同 origin 复用时才串。

## 四、结论：一般无需代码级隔离

不建议为游乐场历史新增类似 `SESSION_COOKIE_NAME` 的隔离开关，原因：

1. 游乐场历史是纯本地 UI 数据，从不发后端、不涉及登录/鉴权，串掉只是看到几条旧消息，**无安全或正确性风险**（cookie 串掉是登录态错乱，性质严重得多）。
2. storage key 写死且无 per-instance 参数化，为 UI 缓存加隔离机制属于过度设计。

需要分开时，按成本从低到高：

- **换个 origin 访问**（不同端口 / 不同子域名）→ localStorage 自动隔离，零改动，最推荐；
- DevTools 或游乐场清空按钮手动删除 `playground_messages`（及另两个 key）；
- 代码层给 storage key 加实例前缀——过度设计，不建议。

## 五、相关源码位置速查

- 键名定义：`web/default/src/features/playground/constants.ts`（`STORAGE_KEYS`）
- 读写 / 清空：`web/default/src/features/playground/lib/storage/storage.ts`
- 容量限制与 Zod schema：`web/default/src/features/playground/lib/storage/storage-schema.ts`
- 加载 / 防抖保存 / flush：`web/default/src/features/playground/hooks/use-playground-state.ts`
