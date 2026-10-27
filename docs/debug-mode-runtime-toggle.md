# Debug 模式运行时开关

## 背景

Debug 模式(`common.DebugEnabled`)会让转发链路把**完整请求体(含 `messages` 会话历史、system 提示)和上游响应**以明文 JSON 记录下来。它用于排查缓存命中率、会话内容、上游行为等问题。

此前该开关**只能在部署时通过环境变量 `DEBUG=true` 设置**,启动后无法更改。这带来两个问题:

- 默认开启日志体积巨大(全量明文)。
- 关闭状态下想临时排查,必须改环境变量并重启/重新部署,无法即时开启。

本次改动把它接入运行时 option 系统,管理员可通过 option 接口**即开即关、无需重启**(控制台暂无可视化开关,见「使用」)。另外,为避免 Debug 日志刷爆 docker logs,DEBUG 级日志改为写入专用文件(见「Debug 日志输出」)。

## Debug 日志输出(专用文件)

为避免 Debug 开启后日志量激增、刷爆 docker logs(stdout/stderr),**所有 DEBUG 级日志(`logger.LogDebug` / `logger.LogJson`)单独写入专用文件 `debug-*.log`,不再进入 stdout/stderr**。

- **默认不产生**:文件是否生成完全跟随 `DebugEnabled` 开关,默认关闭;开启后在**首次写 DEBUG 时惰性创建**。不开 Debug 就不会有 debug 文件。
- **输出目录**:由命令行参数 `--log-dir`(默认 `./logs`)决定,DEBUG 行落到 `<log-dir>/debug-<时间戳>.log`,与常规日志 `oneapi-*.log` 分开存放,且**不**混进 docker logs。**文件名由时间戳自动生成,不可自定义**;要换位置改 `--log-dir` 即可(注意是启动参数,不是环境变量)。
- **回退**:`--log-dir` 为空字符串时,DEBUG 行回退到 stderr(docker logs),并打印一次告警。
- **轮转**:按行数轮转(与主日志一致,阈值 `maxLogCount` = 100 万行);超过后新建 `debug-<新时间戳>.log`,**旧文件保留在磁盘上不自动删除**,因此总量不设上限,需自行留意磁盘占用(可用下面的管理台清理)。
- **管理台**:`debug-*.log` 与 `oneapi-*.log` 一样纳入管理台「日志文件」列表与清理,可在系统设置里查看大小、按个数/天数清理;当前正在写入的 debug 文件会被跳过不删。
- **分流范围**:`common/redis.go` 等处用 `common.SysLog` + `if DebugEnabled` 打的系统级调试行(如 Redis GET/SET)属常规系统日志,仍走 stdout/stderr 与 `oneapi-*.log`,不在本次分流范围内。
- **实现**:`logger/logger.go`(`writeDebugLine` / `openDebugLogLocked`,`logHelper` 对 `loggerDebug` 分流);管理台收集/清理见 `controller/performance.go`(`getLogFiles` / `CleanupLogFiles`,按前缀 `oneapi-`/`debug-` 收集、按修改时间降序)。

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
- **系统访问令牌(Access Token)**:在「个人设置」生成系统访问令牌,放到 `Authorization` 请求头里。注意这是**系统访问令牌**,不是用于转发的 API Key(`sk-...`)。走访问令牌时**必须同时带 `New-Api-User` 头**,值为该令牌所属用户的 id(默认超管通常是 `1`),且该用户须为 **root**、id 要与令牌所属用户一致,否则分别报 `New-Api-User header not provided` / `user id mismatch`。

### 开启

```bash
curl -X PUT 'https://<your-host>/api/option/' \
  -H 'Authorization: <root-access-token>' \
  -H 'New-Api-User: 1' \
  -H 'Content-Type: application/json' \
  -d '{"key": "DebugEnabled", "value": true}'
```

### 关闭

```bash
curl -X PUT 'https://<your-host>/api/option/' \
  -H 'Authorization: <root-access-token>' \
  -H 'New-Api-User: 1' \
  -H 'Content-Type: application/json' \
  -d '{"key": "DebugEnabled", "value": false}'
```

### 确认当前值

```bash
curl 'https://<your-host>/api/option/' \
  -H 'Authorization: <root-access-token>' \
  -H 'New-Api-User: 1'
```

返回的 option 列表里找 `DebugEnabled`,值为字符串 `"true"` / `"false"`。

说明:请求体 `value` 直接传 JSON 布尔 `true`/`false` 即可,`controller.UpdateOption` 会自动转成 `"true"`/`"false"` 字符串存库;写库成功后 `updateOptionMap` 立即把 `common.DebugEnabled` 改为新值,**无需重启**。

## 按 API Key + 模型维度开启（scoped debug）

全局 `DebugEnabled` 是"全开/全关"的总开关,开启期间所有流量都会记录明文,日志量巨大。为支持"只排查某个客户的某个模型",新增了**按 `API Key 名称(令牌名) + 模型` 维度**的作用域开关,默认**全不开启**。

- **配置**:`debug_scope_setting.rules`,一组 `{token_name, model}` 规则。`token_name` 是令牌名(非 `sk-` 原文),`model` 是**客户端请求的模型名**(映射前的原始模型,即 `original_model`)。
- **匹配语义**:两个字段**都必须命中且都非空**(精确相等)。一条规则的 `token_name` 或 `model` 为空时该规则永不生效。要覆盖多个范围请**写多条规则**(规则之间是"或")。
- **生效范围**:命中规则的请求,即使全局 `DebugEnabled=false` 也会记录该请求的转发链路明文(请求/响应体等)。系统级调试行(Redis、迁移日志等)仍只跟随全局开关。
- **令牌名可变的取舍**:令牌名后续被改名后旧规则会失配,代价仅是"本想开 DEBUG 结果没开成",可接受;改名后按新名字补一条规则即可。
- **实现**:判定入口 `logger.DebugActive(ctx)` = `common.DebugEnabled || operation_setting.DebugScopeMatches(tokenName, originalModel)`(从请求上下文读取 `token_name` / `original_model`)。`logger.LogDebug` / `logger.LogJson` 以及少数带 `if common.DebugEnabled` 前置守卫的转发链路日志点都改为走 `DebugActive`;其余未加守卫的 `LogDebug` 自动继承作用域判定。规则结构见 `setting/operation_setting/debug_scope_setting.go`。
- **UI**:系统设置 →「操作设置」→「System Behavior」分区,在 Debug 开关下方,可增删 `API Key + 模型` 规则行。保存走统一的 option 接口(`debug_scope_setting.rules`),持久化与集群同步与其它 option 一致。

## 集群同步

**无需逐台调用,在任意一台机器上调一次即可**,其它节点会自动同步。

- 每个节点启动时都会无条件起后台协程 `go model.SyncOptions(common.SyncFrequency)`(`main.go`),每隔 `SYNC_FREQUENCY` 秒(环境变量,默认 **60s**,`common/init.go`)调用 `loadOptionsFromDatabase()`,把 DB 中**所有 option 全量重载**并应用到本进程全局变量(`model/option.go` `SyncOptions` / `loadOptionsFromDatabase`)。
- 调接口的那台:`UpdateOption` 先写 DB,再立即更新本进程的 `common.DebugEnabled`——**本机瞬时生效**。
- 其它节点:在下一次 sync tick 从 DB 读到新值,**最长约 `SYNC_FREQUENCY` 秒内**(默认 60s)自动跟上;关闭同理。

注意:

- DB 是权威源,因此即使某节点启动时 env 为 `DEBUG=false`,下次 sync 也会被 DB 值覆盖,最终全集群一致。
- 这是 **DB 轮询同步**(非 Redis 实时推送),存在最长一个同步周期的传播延迟。想更快全量生效,可调小 `SYNC_FREQUENCY`,或对每台各调一次(非必须)。
- 由于是全量重载,`DebugEnabled` 作为全局开关在集群内要么全开要么全关,**无法只对单台开启**(单台调用也会在下个周期被 DB 值拉平)。

## 注意事项

- Debug 日志是**客户对话明文全文**,属敏感数据。排查完请及时关闭,并控制 `debug-*.log`(及回退时的 `oneapi-*.log`)的访问权限、留存周期,避免接入会外发日志的第三方采集。
- `debug-*.log` 轮转后旧文件**不会自动删除**,长期开启会持续占用磁盘;请及时用管理台「日志文件」清理,或按天数/个数设置保留策略。
- 该开关仅 **root 权限**可改,防止任意管理员打印全体客户对话。
- 这是**全局**开关,开启期间所有流量都会被记录,日志增长很快,不建议长期开启。
