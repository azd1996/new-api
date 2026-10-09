# Session Cookie 工作流程与多实例浏览器隔离

> 适用场景：在同一台机器、同一 host（如 `localhost`）上通过不同端口部署多套 new-api 实例时，浏览器出现登录态互相覆盖（cookie 冲突）的问题。

## 一、问题背景

浏览器 cookie 的作用域是 **host + path**，**不区分端口**（见 RFC 6265）。因此 `localhost:3000` 与 `localhost:3001` 这两套 new-api 共享同一批 cookie。

new-api 的会话 cookie 名字原先在 `main.go` 中写死为 `"session"`：

```go
server.Use(sessions.Sessions("session", store))
```

两套实例都读写同一个名为 `session` 的 cookie，后登录的会覆盖先登录的，这就是“cookie 冲突 / 登录态串掉”的根因。

注意：`SESSION_SECRET` 只决定 cookie 的**加密/签名密钥**，不改 cookie 名字。两套实例即使设置不同 `SESSION_SECRET`，cookie 名仍相同、仍会互相覆盖；而且名字相同、密钥不同还会导致对方的 cookie 解密失败，反而频繁掉登录。

## 二、涉及的 Cookie 清单

- **后端 session cookie**（核心，登录态所在）
  - 名字：由 `sessions.Sessions(name, store)` 的第一个参数决定（原 `"session"`）。
  - 内容：`id / username / role / status / group` 等，经 `SESSION_SECRET` 签名加密。
  - 属性：`Path=/; Max-Age=2592000(30 天); HttpOnly; SameSite=Strict; Secure=<SESSION_COOKIE_SECURE>`，见 `main.go:217-223`。
  - 这是导致多实例冲突的真正来源。

- **前端 sidebar cookie**（仅 UI 状态，非登录态）
  - 名字：`sidebar_state`，定义于 `web/default/src/components/ui/sidebar.tsx:48`。
  - 内容：侧边栏展开/收起的布尔值；`max-age` 7 天（`SIDEBAR_COOKIE_MAX_AGE`）。
  - 写入点：`sidebar.tsx:106` 通过 `document.cookie` 直接写。
  - 同样跨端口共享，但只影响侧边栏展开状态，不影响登录与安全，可忽略；如需彻底隔离可一并参数化。

- **localStorage（非 cookie，已天然隔离）**
  - `uid`、`status`、`aff`、i18n 语言、前端缓存版本等存于 localStorage（如 `web/default/src/lib/api.ts`、`oauth.ts`、`frontend-cache.ts`）。
  - localStorage 的 origin 包含端口，不同端口天然隔离，**不是**冲突来源。

## 三、后端 session cookie 完整流程

### 3.1 写入（登录时）

1. 登录成功进入 `controller/user.go:144` 的 `setupLogin`：

   ```go
   session := sessions.Default(c)   // 取出本次请求绑定的 session
   session.Set("id", user.Id)       // 写入键值
   session.Set("username", user.Username)
   session.Set("role", user.Role)
   session.Set("status", user.Status)
   session.Set("group", user.Group)
   err := session.Save()            // 关键：触发写回
   ```

2. `session.Save()` 时，`gin-contrib/sessions` 的 cookie store（底层 gorilla/sessions）会：
   - 把这些值序列化并用 `SESSION_SECRET` 签名/加密成一个字符串 value；
   - 按 `main.go` 注册时的配置生成响应头：

     ```
     Set-Cookie: <cookie名>=<加密value>; Path=/; Max-Age=2592000; HttpOnly; SameSite=Strict
     ```

     其中 `<cookie名>` 即 `sessions.Sessions(name, store)` 的第一个参数。

3. 浏览器收到响应，看到 `Set-Cookie` 头，就把该 cookie 存入 cookie jar（按 host+path 归档）。**浏览器此刻才第一次得知这个名字，无需任何预先约定。**

### 3.2 读取（后续每次请求）

1. 浏览器向该 host 发请求时，自动把匹配 path 的 cookie 拼进请求头：

   ```
   Cookie: <cookie名>=<加密value>
   ```

2. `main.go` 注册的 session 中间件在请求早期运行，**按注册时的同一个 name** 去请求头里找 cookie，用 `SESSION_SECRET` 验签/解密，还原成 session 对象。
3. 业务代码再读取，例如鉴权中间件 `middleware/auth.go:38-42`：

   ```go
   session := sessions.Default(c)
   username := session.Get("username")
   id := session.Get("id")
   ```

### 3.3 关键点：写与读用的是同一个 name

`sessions.Sessions(name, store)` 的 `name` 同时决定“下发时的 cookie 名”和“解析时查找的 cookie 名”，二者是同一个变量，天然一致。因此改 name 时写/读会同步变更，不会错位。

## 四、改了 cookie 名，浏览器“知道”吗？

**无需浏览器提前知道**。cookie 名由服务端通过 `Set-Cookie` 单向告知浏览器，浏览器全程被动跟随：

- 新实例下发 `Set-Cookie: newapi_a=...`，浏览器就存 `newapi_a`；另一实例下发 `newapi_b=...` 就存 `newapi_b`。名字不同 → 不再互相覆盖 → 隔离达成。对浏览器完全透明，无需任何前端配置。
- **残留旧 cookie**：改名前浏览器里可能已有旧的 `session` cookie。改名后新实例只读新名字，旧 `session` 会一直留到 `Max-Age`（30 天）过期或手动清除；无害，仅占位。想干净可在首次改名后清一下该站点 cookie。
- **`SESSION_SECRET` 必须对得上**：cookie 名负责“找得到”，`SESSION_SECRET` 负责“解得开”。同一实例重启后 secret 不变才能继续解密旧 cookie；secret 变了即使名字对也会解密失败而掉登录。

## 五、多实例隔离方案与配置

已新增环境变量 `SESSION_COOKIE_NAME`（默认 `session`，向后兼容）。每套实例设不同的 cookie 名即可隔离：

```yaml
# 实例 A
- SESSION_COOKIE_NAME=newapi_a
- SESSION_SECRET=<随机串A>

# 实例 B
- SESSION_COOKIE_NAME=newapi_b
- SESSION_SECRET=<随机串B>
```

补充：
- `SESSION_SECRET` 建议每套各自固定且互不相同（安全性 + 避免跨实例串签）。
- 前端 `sidebar_state` 只是 UI 状态，一般无需隔离；若要隔离需把 `sidebar.tsx` 的 `SIDEBAR_COOKIE_NAME` 改为可配置并各实例取不同值。
- 更彻底的隔离是用不同（子）域名访问而非同 host 不同端口；但同机同 host 场景下，上面的 cookie 名隔离是最小改动方案。

## 六、代码改动清单

- `common/constants.go`：新增 `var SessionCookieName = "session"`。
- `common/init.go`：读取 `SESSION_COOKIE_NAME` 环境变量覆盖默认值（非空时生效）。
- `main.go`：`sessions.Sessions(...)` 由写死的 `"session"` 改为 `common.SessionCookieName`。

## 七、相关源码位置速查

- 注册 session 中间件 / cookie 属性：`main.go:216-224`
- 读取环境变量：`common/init.go`（`SESSION_SECRET` / `CRYPTO_SECRET` / `SESSION_COOKIE_NAME`）
- Secure/Trusted URL 相关：`common/session_cookie.go`
- 登录写入 session：`controller/user.go:144` `setupLogin`
- 鉴权读取 session：`middleware/auth.go:38`
- 前端 sidebar cookie：`web/default/src/components/ui/sidebar.tsx:48,106`
- 前端通用 cookie 工具：`web/default/src/lib/cookies.ts`

