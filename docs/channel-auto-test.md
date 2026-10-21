# 渠道自动测试与自动禁用 / 启用（Channel Auto-Test & Auto Disable/Enable）

> 结论先行：**默认不开启**。未显式配置时，网关不会周期性主动探测自己的渠道。周期性测试、按失败/超时自动禁用、测试通过自动恢复，三者都是独立开关且默认 `false`。

---

## 1. 默认行为（关键结论）

- **周期性测试**：默认关闭。`AutoTestChannelEnabled=false`（`setting/operation_setting/monitor_setting.go:23`）。调度任务的 `Enabled()` 直接返回该值（`controller/system_task_handlers.go:34`），为 false 时系统任务框架根本不注册执行。
- **自动禁用**：默认关闭。`AutomaticDisableChannelEnabled=false`（`common/constants.go:153`）。
- **自动启用**：默认关闭。`AutomaticEnableChannelEnabled=false`（`common/constants.go:154`）。
- **余额更新**（易混淆）：`AutomaticallyUpdateChannels`（`main.go:124-128`）只有设置了 `CHANNEL_UPDATE_FREQUENCY` 才跑，且它查的是渠道余额，**不是可用性探测**。

## 2. 配置项与默认值

全部挂在「系统设置 - 模型 - 路由可靠性 / 监控」。后端默认值见下，前端表单在 `web/default/.../models/routing-reliability-section.tsx`（classic 为 `web/classic/.../SettingsMonitoring.jsx`）。

- `monitor_setting.auto_test_channel_enabled`（默认 `false`）—— 周期性测试总开关。
- `monitor_setting.auto_test_channel_minutes`（默认 `10`）—— 测试周期（分钟）。
- `monitor_setting.channel_test_mode`（默认 `scheduled_all`）—— 见 §4。
- `AutomaticDisableChannelEnabled`（默认 `false`）—— 测试/真实请求失败是否自动禁用渠道。
- `AutomaticEnableChannelEnabled`（默认 `false`）—— 被自动禁用的渠道测试通过后是否自动恢复。
- `ChannelDisableThreshold`（默认 `5.0` 秒）—— 测试响应时间超过此值视为失败（仅在自动禁用开启时生效）。

**环境变量覆盖**（`monitor_setting.go:34-47`）：

- `CHANNEL_TEST_FREQUENCY=<分钟数>`：>0 时自动把开关置 true、周期设为该值、模式设为 `scheduled_all`。
- `CHANNEL_TEST_ENABLED=<bool>`：显式覆盖总开关（可单独开/关，不改周期）。

## 3. 调度与执行链路（系统任务框架）

周期性测试不再用裸 goroutine，而是走系统任务框架，便于集群去重与留痕：

```
RegisterScheduledSystemTasks()  (controller/system_task_handlers.go:20)
    → 注册 channelTestHandler
        Enabled()  = GetMonitorSetting().AutoTestChannelEnabled
        Interval() = AutoTestChannelMinutes 分钟（<=0 回退 10）
    → service.StartSystemTaskRunner 按 Interval 触发
        → DB 租约（按任务类型加锁）在多 master 实例间去重，一次执行落一行任务记录
    → channelTestHandler.Run → runChannelTestTask(ctx, mode="", notify=false)
```

**手动「测试所有渠道」**（`TestAllChannels`，`controller/channel-test.go:1035`）：入队一个 `mode=scheduled_all, notify=true` 的同类型系统任务；若已有同类任务在跑/排队则返回 409 拒绝，避免把计划任务误当成本次手动任务。

`runChannelTestTask`（`channel-test.go:997`）：取全部渠道 → `selectChannelsForAutomaticTest` 按模式筛选 → `allowDisable = mode != passive_recovery` → `performChannelTests` 逐个同步测试（尊重 ctx 取消，runner 丢租约即停）→ notify 时完成后通知 root。

## 4. 两种测试模式（ChannelTestMode）

- `scheduled_all`（默认）：测试所有「非手动禁用」的渠道；`allowDisable=true`，失败/超时可触发自动禁用。
- `passive_recovery`：**只测已被自动禁用**（`ChannelStatusAutoDisabled`）的渠道；`allowDisable=false`，只做恢复、不再禁用。用于“只探活、让恢复的渠道回归”的保守场景。

筛选逻辑见 `selectChannelsForAutomaticTest`（`channel-test.go:1018`）：始终跳过 `ChannelStatusManuallyDisabled`；`passive_recovery` 额外跳过非自动禁用的渠道。

## 5. 禁用 / 启用判定（performChannelTests）

对每个渠道发一次真实测试请求（是否流式由 `shouldUseStreamForAutomaticChannelTest` 决定），然后：

- **是否该禁用**：先 `service.ShouldDisableChannel(err)`（`service/channel.go:45`，受 `AutomaticDisableChannelEnabled` 门控：渠道类错误 / 命中可禁用状态码 / 命中关键词才 true）。若无错且自动禁用开启，再判响应耗时 `> ChannelDisableThreshold*1000ms` → 记为超时错误并标记禁用（`channel-test.go:944-950`）。
- **执行禁用**：需同时满足 `allowDisable && 渠道当前启用 && shouldBan && channel.GetAutoBan()`，走 `processChannelError`。
- **执行启用**：`result.localErr==nil && 渠道当前非启用 && ShouldEnableChannel(...)`。`ShouldEnableChannel`（`service/channel.go:67`）受 `AutomaticEnableChannelEnabled` 门控，且只恢复 `ChannelStatusAutoDisabled`（手动禁用的不会被自动拉起）。
- 无论启停，都 `UpdateResponseTime` 并在 `RequestInterval>0` 时节流 sleep。

## 6. 被动自动禁用（真实请求链路，非测试触发）

即便从不开启周期测试，**自动禁用开关一旦打开**，真实转发失败也会触发禁用——这是独立于 §3 调度的被动路径：

- 转发出错时 `processChannelError → service.DisableChannel`（`service/channel.go:19`），同样要求该渠道 `AutoBan` 为真。
- mjproxy 等路径：`channel.GetAutoBan() && common.AutomaticDisableChannelEnabled`（`relay/mjproxy_handler.go:598`）。

## 7. 通知

- 渠道被自动禁用 / 启用：`DisableChannel` / `EnableChannel` 各自 `NotifyRootUser`（按 `channelId_status` 去重键）。
- 手动「测试所有渠道」完成：`NotifyRootUser(NotifyTypeChannelTest, ...)`。
- 计划任务（notify=false）默认不发完成通知。

## 8. 关键文件索引

- `setting/operation_setting/monitor_setting.go` — 周期测试开关/周期/模式 + 环境变量覆盖
- `common/constants.go` — `AutomaticDisableChannelEnabled` / `AutomaticEnableChannelEnabled` / `ChannelDisableThreshold`
- `controller/system_task_handlers.go` — `channelTestHandler`（Enabled/Interval/Run）
- `controller/channel-test.go` — `runChannelTestTask` / `performChannelTests` / `selectChannelsForAutomaticTest` / `TestAllChannels`
- `service/channel.go` — `DisableChannel` / `EnableChannel` / `ShouldDisableChannel` / `ShouldEnableChannel`
- `main.go` — `AutomaticallyUpdateChannels`（余额更新，`CHANNEL_UPDATE_FREQUENCY`，非探活）
- 前端：`web/default/src/features/system-settings/models/routing-reliability-section.tsx`、`web/classic/src/pages/Setting/Operation/SettingsMonitoring.jsx`

