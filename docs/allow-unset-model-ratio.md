# 未配置价格模型放行开关（Allow Unset Model Ratio）

## 背景

模型价格/倍率未配置时，请求会被 `relay/helper/price.go` 直接拒绝（`400`，错误信息「模型 X 的价格未配置 / price not configured」）。判定链是「固定价格 → 模型倍率 → 用户级许可标志」三段兜底：

- `GetModelPrice`（`setting/ratio_setting/model_ratio.go`）未命中返回 `(-1, false)`。
- `GetModelRatio` 未命中返回 `(37.5, operation_setting.SelfUseModeEnabled, name)`——即是否成功取决于全局「自用模式」。
- 两者都未命中时，仅当用户设置 `AcceptUnsetRatioModel` 为真才放行，否则报错。

此前要全局放行未配价模型，只能开「自用模式（`SelfUseModeEnabled`）」，但该开关语义绑定自用场景（还影响模型列表默认值等），不够独立。

## 目标

新增一个**独立的全局开关**，开启后未配置价格的模型也能正常访问，与「自用模式」解耦。

## 关键决策

- **复用既有 accept-unset 分支**，不新增计费路径：放行条件由 `info.UserSetting.AcceptUnsetRatioModel` 改为 `info.UserSetting.AcceptUnsetRatioModel || operation_setting.AllowUnsetModelRatio`。
- **计费采用兜底倍率 37.5**（`GetModelRatio` 未命中时的返回值）× 分组倍率，与「自用模式/用户级放行」行为完全一致（方案 A）。未选择「免费(0)」，以免无意间免费放行。
- **不新增数据库列**：开关走既有 `options` K-V 表持久化，重启后保留。
- 已配价/已配倍率的模型不受影响，仍按各自设置计费。

## 实现

### 后端

- `setting/operation_setting/operation_setting.go`：新增全局变量 `AllowUnsetModelRatio`（默认 `false`）。
- `model/option.go`：
  - 初始化处 `common.OptionMap["AllowUnsetModelRatio"] = strconv.FormatBool(operation_setting.AllowUnsetModelRatio)`。
  - `updateOptionMap` 的 switch 增加 `case "AllowUnsetModelRatio"`，从 DB 读回同步到内存变量。
- `relay/helper/price.go`：`ModelPriceHelper`（chat/completions/responses 等）与 `ModelPriceHelperPerCall`（MJ/Task 按次按量）两处，`acceptUnsetRatio` 判定并入全局开关。

### 前端（运营设置 → System Behavior）

- `features/system-settings/types.ts`：`OperationsSettings` 增加 `AllowUnsetModelRatio: boolean`。
- `operations/index.tsx`：`defaultOperationsSettings` 增加默认值 `false`。
- `operations/section-registry.tsx`：`behavior` 段向 `SystemBehaviorSection` 传入该值。
- `general/system-behavior-section.tsx`：zod schema 增加布尔字段，并新增一个 `Switch`（复用已有「保存」按钮，仅提交有变更的键，走 `PUT /api/option/`）。
- i18n：新增 2 条文案（`Allow Models Without Configured Price` 及其说明），en/zh/zh-TW/fr/ja/ru/vi 均补齐。

## 验证

- `go build`（relay/model/setting/controller）通过；`gofmt -l` 无改动；`go vet ./relay/helper` 干净。
- 前端 `bun run typecheck` 通过；改动文件 `oxlint` 0 error。

## 效果与注意

- 关闭（默认）：行为不变，未配价模型仍返回 `400`。
- 开启：未配价模型放行，按兜底倍率 `37.5 × 分组倍率` 计费。
- 与「自用模式」「用户级 `AcceptUnsetRatioModel`」为「或」关系，任一满足即放行。
- 本页文案构建期打包进维护页，前端改动需 `bun run build` 后生效。
