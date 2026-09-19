# 合并 upstream/main 到 feature/custom-query-api 注意事项

- 生成日期：2026-09-19
- merge-base：`a63364d1`
- 分支关系：本分支领先 main **10** 个提交（自研：csl-hourly / logshipper / redis 前缀 / relayBasePath），main 领先本分支 **282** 个提交
- 冲突判定方式：`git merge-tree --write-tree main HEAD`（内存试合并，未改动工作区），下列冲突为真实结果

## 一、冲突文件清单

### A. 真实冲突，需手动解决（10 个）
- `.gitignore`
- `README.md` / `README.en.md` / `README.fr.md` / `README.ja.md` / `README.zh_CN.md` / `README.zh_TW.md`（7 个）
- `go.mod`
- `go.sum`
- `main.go` ← 唯一需要人工判断业务逻辑的代码文件

### B. 自动合并成功，但必须复核语义（7 个）
- `model/log.go` ← **高优先级**，upstream 重写该文件（+97/-122），我方新增了 shipLog 双写
- `common/constants.go` / `common/init.go` / `common/redis.go`
- `docker-compose.yml` / `docker-compose.dev.yml`
- `router/api-router.go`

> 说明：git 能"无冲突"文本合并，不代表语义正确。B 类文件必须编译 + 跑测试确认。

### C. 仅本分支改动，无冲突（其余）
`controller/csl_hourly.go`、`middleware/csl_hourly_auth.go`、`model/csl_hourly.go`、`pkg/logshipper/*`、`router/relay_base_path.go` 及各自 `_test.go` —— upstream 未触碰。

## 二、逐文件注意事项

### main.go（真实冲突，重点）
upstream 侧大改：
- 前端 embed：`web/default/dist` → `web/dist`，删除 classic 前端 embed
- `SetRouter` 签名：`ThemeAssets` → `WebAssets`（去掉 classic 字段）
- 删除 session store 初始化块（`gin-contrib/sessions` 整体移除）
- 新增：jsplugin CLI 分发、kitutil 日志设置、`wsmanager.StartSubscriber`、`controller.SyncTaskPlugins`、`middleware.ConfigureTrustedProxies`
- `InitResources` 新增：密码加密初始化、`MigrateRetiredFrontendOptions`、`StartAuthArtifactCleanup`

我方需保留并重新落位（采用 upstream 新代码后再叠加）：
1. import：`pkg/logshipper`
2. defer 中 `logshipper.Close()` —— 注意 upstream 的 defer 块没有这段，需手动重新插入
3. HTTP server：`handler := router.WrapRelayBasePath(relayBasePath(), server)`，`http.Server.Handler` 用 `handler`。此区域 upstream 改了 `SetRouter`、删了 session，需在采用 upstream 新代码后再包一层
4. `relayBasePath()` 函数（新增，本身无冲突，但位于冲突区）
5. `InitResources` 中的 `logshipper.Init` 块，与 upstream 新增的初始化并存

### go.mod / go.sum（真实冲突）
- upstream 关键变化：新增本地模块 `replace github.com/QuantumNous/new-api/relaykit => ./relaykit`；ClickHouse 升 2.46；**删除 `gin-contrib/sessions`**；新增 `openai-go` / `sobek` / `miniredis` / `go-oidc` 等；gin 保持 `1.9.1`；gorm 升 `1.25.12`
- 我方变化：gin 升 `1.12.0`、validator 升 `10.30.1`、新增 `gopkg.in/natefinch/lumberjack.v2`（logshipper 依赖）等
- 解决策略：**以 upstream 的 go.mod 为基线**，只补回 `gopkg.in/natefinch/lumberjack.v2`，放弃我方顺带的版本 bump（gin 等交给 upstream 版本），随后 `go mod tidy` 重新生成 go.sum
- 注意：确认 logshipper 不依赖被 upstream 删除的包；确认放弃 gin 1.12.0 后自研代码仍编译

### model/log.go（自动合并成功，高风险复核）
- 我方仅新增：`createLog` 内的 `shipLog(log)` 调用 + `shipLog` 辅助函数 + `pkg/logshipper` import
- upstream 重写该文件（+97/-122），`createLog` / `Log` 结构 / 字段命名可能变动
- 复核点：
  - 合并后 `createLog` 是否仍存在，`shipLog(log)` 是否落在 `LOG_DB.Create` 成功**之后**
  - `logshipper.Row` 引用的字段（`log.Group` / `log.Ip` / `log.Other` / `UpstreamRequestId` 等）在新 `Log` 结构中是否仍同名存在
  - 编译 + 跑 model 相关测试确认

### common/constants.go / init.go / redis.go（自动合并，复核）
- 确认 redis key 前缀逻辑与 upstream 的 redis 相关改动语义兼容，前缀仍作用于**全部** key
- 确认 `constants.go` / `init.go` 中我方新增的 LogShipper 配置项未与 upstream 新增项撞名

### README ×7 + .gitignore（真实冲突，机械）
- 保留双方内容即可
- ⚠️ **AGENTS.md 硬性要求**：不得删除/修改 `new-api`、`QuantumNous` 相关品牌、署名、元数据。解决 README 冲突时必须保留 upstream 的这些标识

## 三、建议合并步骤
1. 建实验分支：`git switch -c merge/upstream-main`
2. `git merge main`
3. 解冲突顺序：先 `main.go`（逻辑）→ 再 `go.mod`（取 upstream 基线 + 补 lumberjack）→ README / `.gitignore`（保留双方 + 护住品牌署名）
4. `go mod tidy` 重新生成 go.sum
5. `go build ./...`
6. 复核 B 类自动合并文件语义，重点 `model/log.go`
7. 跑测试：`go test ./...`（重点 model、pkg/logshipper、router、middleware、common）
8. 手动验证：`RELAY_BASE_PATH` 白名单、logshipper 双写落文件、redis 前缀、csl_hourly 接口

## 四、合并后验证清单
- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过（自研模块全绿）
- [ ] logshipper 启用时正常写文件、关闭时 `Close` 无错
- [ ] `relayBasePath` 包装后路由白名单生效，web 静态资源（`web/dist`）正常加载
- [ ] redis key 前缀仍全量生效
- [ ] README 品牌署名（new-api / QuantumNous）完好

## 五、本次合并已完成的事项
- 10 个冲突文件全部解决，无残留冲突标记：`main.go`（import 块保留双方）、`go.mod`（upstream 基线 + lumberjack）、`go.sum`（tidy 重生成）、`.gitignore`（保留双方）、7 个 README（采用 upstream 规范版本，品牌署名完好）
- 已验证通过：`go build ./common/...`、`cd relaykit && GOWORK=off go build ./...`
- 折叠本地 `third_party` 后整树 `go mod tidy` 干净、`go build ./...` 仅剩 2 个环境前置报错（见下），其余包（model / controller / router / middleware 等）均编译通过

## 六、收尾步骤（合并后仍需完成）
两个报错均为环境前置条件，非合并引入：

1. **重跑 csl-logshipper 同步脚本**：本地 `third_party/csl-logshipper` 是旧版同步（模块路径仍是内部名），导致 `writer.go` 内部 import 无法解析。重跑 `docker/sync-csl-logshipper.sh` 刷新为 QuantumNous 路径版本。该目录为 gitignored，不进入提交。
2. **构建前端产物**：upstream 把 embed 从 `web/default/dist` 改成 `web/dist`。执行 `cd web && bun install && bun run build` 生成 `web/dist`，否则 `main.go` 的 `//go:embed web/dist` 报 `no matching files found`。

完成上述两步后：
- `go build ./...` 应完整通过
- `go test ./...` 做完整回归（重点 model、pkg/logshipper、router、middleware、common）
- 手动验证：`RELAY_BASE_PATH` 白名单、logshipper 双写落文件、redis 前缀、csl_hourly 接口
