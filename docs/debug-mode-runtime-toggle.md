# Debug 模式运行时开关

## 背景

Debug 模式(`common.DebugEnabled`)会让转发链路把**完整请求体(含 `messages` 会话历史、system 提示)和上游响应**以明文 JSON 打到日志里(stdout/stderr,设了 `LOG_DIR` 时同时落 `oneapi-*.log`)。它用于排查缓存命中率、会话内容、上游行为等问题。

此前该开关**只能在部署时通过环境变量 `DEBUG=true` 设置**,启动后无法更改。这带来两个问题:

- 默认开启日志体积巨大(全量明文)。
- 关闭状态下想临时排查,必须改环境变量并重启/重新部署,无法即时开启。

本次改动把它接入运行时 option 系统,管理员可通过 option 接口**即开即关、无需重启**(控制台暂无可视化开关,见「使用」)。

## 行为

- `DEBUG=true` 环境变量仍然有效,作为**启动默认值**。
- 管理台修改后的值会**持久化到数据库**,并在之后的启动中覆盖环境变量默认值(与其他 `*Enabled` option 一致)。
- 修改**立即生效**(改的是进程内全局变量),无需重启。

启动与覆盖顺序(`main.go`):`common.InitEnv()`(读 `DEBUG` 环境变量)→ `model.InitOptionMap()`(用环境变量值播种 OptionMap,随后 `loadOptionsFromDatabase` 用 DB 持久化值覆盖)。

## 实现

接入点与其他布尔 option 完全一致(key 以 `Enabled` 结尾,自动命中布尔分发):

- 全局变量声明:`common/constants.go` — `var DebugEnabled bool`
- 环境变量默认值:`common/init.go` — `DebugEnabled = os.Getenv("DEBUG") == "true"`
- 播种默认值:`model/option.go` `InitOptionMap()` — `common.OptionMap["DebugEnabled"] = strconv.FormatBool(common.DebugEnabled)`
- 运行时写回:`model/option.go` `updateOptionMap()` 布尔 switch — `case "DebugEnabled": common.DebugEnabled = boolValue`
- 管理接口(共用):`PUT /api/option/`(`router/api-router.go`,`RootAuth` 保护)

## 使用(调接口切换)

> 说明:**控制台暂无对应的可视化开关**,系统设置页是逐个手写控件渲染的,没有为该 option 加 UI。因此只能通过下面的 option 接口切换。

接口:`PUT /api/option/`,路由在 `router/api-router.go`,受 `middleware.RootAuth()` 保护——**必须是 root 账号**。

### 鉴权

两种任选其一(见 `middleware/auth.go` `authHelper`):

- **管理员会话 Cookie**:在浏览器里以 root 登录后,带上会话 Cookie 调用(例如用浏览器开发者工具 / 已登录的同源页面)。
- **系统访问令牌(Access Token)**:在「个人设置」生成系统访问令牌,放到 `Authorization` 请求头里。注意这是**系统访问令牌**,不是用于转发的 API Key(`sk-...`)。

### 开启

```bash
curl -X PUT 'https://<your-host>/api/option/' \
  -H 'Authorization: <root-access-token>' \
  -H 'Content-Type: application/json' \
  -d '{"key": "DebugEnabled", "value": true}'
```

### 关闭

```bash
curl -X PUT 'https://<your-host>/api/option/' \
  -H 'Authorization: <root-access-token>' \
  -H 'Content-Type: application/json' \
  -d '{"key": "DebugEnabled", "value": false}'
```

### 确认当前值

```bash
curl 'https://<your-host>/api/option/' \
  -H 'Authorization: <root-access-token>'
```

返回的 option 列表里找 `DebugEnabled`,值为字符串 `"true"` / `"false"`。

说明:请求体 `value` 直接传 JSON 布尔 `true`/`false` 即可,`controller.UpdateOption` 会自动转成 `"true"`/`"false"` 字符串存库;写库成功后 `updateOptionMap` 立即把 `common.DebugEnabled` 改为新值,**无需重启**。

## 注意事项

- Debug 日志是**客户对话明文全文**,属敏感数据。排查完请及时关闭,并控制 `oneapi-*.log` 的访问权限、留存周期,避免接入会外发日志的第三方采集。
- 该开关仅 **root 权限**可改,防止任意管理员打印全体客户对话。
- 这是**全局**开关,开启期间所有流量都会被记录,日志增长很快,不建议长期开启。
