# 集群部署下单实例重启的请求损失分析

> 面向运维场景:newapi 多实例集群部署,重启(或滚动升级)其中单个实例时,对用户请求的影响程度与规避方法。代码事实截至分支 `feature/custom-query-api`。

## 结论先行

- 单实例重启在**优雅关闭 + 负载均衡正确摘流**前提下,请求损失接近于零。
- 真正的损失几乎都来自两个**可控**因素:
  1. 编排器(Docker/K8s)的 kill 宽限期短于应用的优雅关闭超时 `SHUTDOWN_TIMEOUT_SECONDS`;
  2. 超长流式会话时长超过该超时被强制截断。
- 本部署以 Claude Code 编程长会话、Claude 原生 `/v1/messages` 流式为主,对第 2 点敏感,需重点关注。

## 关闭机制(代码事实)

关闭链路见 `main.go:256-272`:

- 监听 `SIGINT` / `SIGTERM`,收到后调用 `srv.Shutdown(ctx)`——Go 标准优雅关闭:**停止接收新连接,等待在途请求自然结束**。
- 等待上限由环境变量 `SHUTDOWN_TIMEOUT_SECONDS` 控制,**默认 120 秒**。代码注释:`SSE streams may run for minutes; give them time to finish before forced exit`。
- 关闭完成后,若开启 `DataExportEnabled`,调用 `model.SaveQuotaDataCache()` 把看板数据落库。
- 健康检查端点为 `/api/status`(`controller.GetStatus`),供 LB / 编排器判活(见 `router/relay_base_path.go:71`)。

## 分维度损失分析

### 1. 打到该实例的"新请求"
- 风险窗口 = LB 把该实例摘出健康列表之前仍转发过来的请求。
- `Shutdown` 一旦触发即拒绝新连接,窗口内新请求收到连接拒绝 / 502。
- 损失程度**取决于 LB 摘流是否先于进程退出**;做好 readiness 探针 + preStop 延迟即可降到 0。

### 2. 该实例上的"在途请求"(关键)
- 普通短请求:基本都能在 120s 内自然完成,**无损失**。
- 流式请求(主力 `/v1/messages`):`Shutdown` 会等待 handler 返回。单轮响应通常远小于 120s,能平滑结束;但**单次响应时长超过 `SHUTDOWN_TIMEOUT_SECONDS` 的会在超时时被强制切断**,客户端表现为流中途断开 / 连接重置。Claude Code 的单轮长输出有可能触及该边界。

### 3. 编排器 kill 宽限期(最常见的真实损失来源)
- `docker stop` 默认宽限 10s、K8s `terminationGracePeriodSeconds` 默认 30s,**都远小于应用默认的 120s**。
- 后果:SIGTERM 后应用还在等在途请求,编排器已 `SIGKILL`,**所有在途请求(含全部流式会话)被硬切**,`SaveQuotaDataCache` 也不执行。
- 这是配置错配,不是应用缺陷,务必对齐。

### 4. 计费 / 用量数据
- 开启 `BATCH_UPDATE_ENABLED` 时(`model/utils.go`):用户 / 令牌 / 渠道用量在内存累加,每 `BatchUpdateInterval` 秒批量刷库。**关闭路径未做最后一次 `batchUpdate()` 强制 flush**(`main.go` 关闭段只调了 `SaveQuotaDataCache`),故**自上次刷库到关闭之间累积的用量增量会丢失**(方向上是少计费,对用户无感、对运营是少量账面损失);SIGKILL 场景丢得更多。
- 不开批量更新时每请求实时写库,无此问题。
- 单条消费日志(`RecordConsumeLog`)在请求结束时写入,请求被硬切则该条日志不产生。

### 5. 异步任务(MJ / Suno / 视频)
- 基于 DB 租约在 master 间去重调度(`main.go:152-158`)。单实例重启不影响任务状态,下一轮询周期由存活节点接管,**最多延迟,不丢任务**。

### 6. 共享状态(Redis)
- 限流计数、token / user 配额缓存等在 Redis,**跨实例共享,重启不丢**。内存缓存(渠道、配置)重启后自动重建。

## 损失程度速查

- **零损失**:异步任务、Redis 共享状态、已完成请求、关闭前已摘流的新请求。
- **可能丢失**:在途超长流式会话(超 `SHUTDOWN_TIMEOUT_SECONDS`);批量模式下未 flush 的用量增量。
- **放大损失(需避免)**:编排器宽限期 < `SHUTDOWN_TIMEOUT_SECONDS` 导致 SIGKILL,在途请求全部被硬切。

## 运维建议(按收益排序)

1. **对齐宽限期**:让编排器宽限 ≥ `SHUTDOWN_TIMEOUT_SECONDS`。K8s 设 `terminationGracePeriodSeconds` 略大于该值;Docker 用 `--stop-timeout`。否则 120s 优雅关闭形同虚设。
2. **先摘流再停**:readiness 探针指向 `/api/status`,加 `preStop` sleep(或先从 LB 注销、等探针失败)再发停止信号,消除"新请求窗口"。
3. **滚动重启**:集群逐实例 rolling restart,任一时刻只摘一个节点,整体不中断。
4. **按长会话调 `SHUTDOWN_TIMEOUT_SECONDS`**:针对 Claude Code 长输出可适当上调(同时相应上调编排器宽限期),减少流式截断。
5. **计费取舍**:开 `BATCH_UPDATE_ENABLED` 则接受少量用量丢失;对账面敏感时改用实时写库(关闭批量),或在关闭路径补一次 batch 强制 flush。

## 配置样例

K8s(关键片段):

```yaml
spec:
  terminationGracePeriodSeconds: 150   # ≥ SHUTDOWN_TIMEOUT_SECONDS(默认120)
  containers:
    - name: new-api
      env:
        - name: SHUTDOWN_TIMEOUT_SECONDS
          value: "120"
      readinessProbe:
        httpGet:
          path: /api/status
          port: 3000
        periodSeconds: 5
        failureThreshold: 2
      lifecycle:
        preStop:
          exec:
            command: ["sh", "-c", "sleep 10"]   # 等 LB 摘流后再进入优雅关闭
```

Docker:

```bash
# 停止宽限期需 ≥ SHUTDOWN_TIMEOUT_SECONDS
docker stop --time=150 <container>
```

## 关键代码位置

- 优雅关闭与超时:`main.go:256-272`
- 看板数据落库:`model.SaveQuotaDataCache`
- 批量用量更新:`model/utils.go`(`InitBatchUpdater` / `batchUpdate`)
- 异步任务 DB 租约调度:`main.go:152-158`、`service.StartSystemTaskRunner`
- 健康检查端点:`router/api-router.go:25`(`/api/status`)

