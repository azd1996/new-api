# 管理员新建模型会覆盖已有价格(回到「未设置价格」)

## 现象

1. 创建渠道,在渠道内添加模型(例:`claude-opus-5`)。
2. 在「系统设置 - 计费与支付 - 模型定价」中对该未定价模型做**上游价格同步**。完成后 `claude-opus-5` 在该渠道可用(已有价格)。
3. 在「管理员 - 模型」页**新建** `claude-opus-5`。提交后,原先同步得到的价格被抹掉,模型回到「未设置价格」状态。

## 结论

这是**前端 bug**,不是后端问题。

- 后端新建模型 `POST /api/models/` → `CreateModelMeta`(`controller/model_meta.go:89`)只写 `models` 元数据表,并调用 `RefreshPricing()` 重建只读缓存(`model/pricing_refresh.go`),**不写任何价格数据**。
- 价格独立存放在 `options` 表的 `ModelPrice` / `ModelRatio` / `CompletionRatio` 等 JSON map 中,由 `ratio_setting` 维护。
- 真正覆盖价格的是前端新建模型抽屉的提交逻辑:它在创建成功后,会把整份定价 map 重新序列化、通过 `PUT /api/option/` **整体写回**。

## 根因(前端)

文件:`web/default/src/features/models/components/drawers/model-mutate-drawer.tsx`,`onSubmit`。

1. **新建模式不回填已有价格**:`:386-411` 对新建场景把 `price` 和各 `ratio` 一律 reset 成 `''`。这与编辑模式不同——编辑模式会从 `modelSettings` 读取该模型已有价格并回填(`:310-379`)。
2. **提交时按表单字段判断 `hasRatioConfig`**:`:447-457`。新建时只填了模型名、没填价格,故 `hasRatioConfig = false`。
3. **无条件把当前模型名从所有价格 map 删除**:`:503-511`
   ```
   delete priceMap[finalModelName]
   delete ratioMap[finalModelName]
   ...
   ```
   因 `hasRatioConfig` 为 false,`:513-554` 不会把价格重新加回去。
4. **把删掉条目后的 map 整体写回**:序列化结果与快照不同 → push 进 updates(`:559-563`)→ 逐条 `PUT /api/option/`(`:621-623`)。后端 `UpdateOption`(`controller/option.go` → `model.UpdateOption` → `ratio_setting.UpdateModelPriceByJSONString`)对 `ModelPrice` 是**整体替换**,于是 `claude-opus-5` 的价格条目被抹除 → 「未设置价格」。

一句话:**只要某个模型名在定价 map 里已有价格(无论来自上游同步还是手动),在「管理员 - 模型」页用相同模型名新建/登记且不重新填价格,提交就会删掉这条价格。**

## 附带隐患:快照过期

`modelSettings` 来自 `useSystemOptions()`,有 5 分钟 `staleTime`(`web/default/src/features/system-settings/hooks/use-system-options.ts:23-29`),提交前不 refetch。即便用户填了价格,若这份缓存是上游同步**之前**的旧快照,整体写回还会把**其他**模型刚同步的新价格一并覆盖。

## 影响范围

- 任何「模型名已有价格 + 在模型页以同名新建/登记」的组合。
- 典型触发:先用上游同步补齐价格,再去模型页登记该模型做展示/分组配置。

## 修复方向(未实现)

二选一或都做:

- 新建模式也从 `modelSettings` 回填该模型名已有价格(对齐 `:310-379` 编辑模式逻辑),使 `hasRatioConfig` 为 true,价格原样写回。
- 更稳妥:提交前对 `['system-options']` 做一次 refetch 拿最新 map;并且仅在用户**确实修改了定价**时才下发对应 option 更新,避免「删除后不回填」以及「用旧快照整体覆盖」。
