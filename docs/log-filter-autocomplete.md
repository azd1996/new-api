# 日志筛选模型/分组自动候选（Log Filter Autocomplete）

## 背景

通用日志（使用日志）筛选栏里，「模型名称」和「分组」是运维最常用的两个条件，原先都是纯文本输入框（`LogsFilterInput` → 普通 `Input`），只能手动逐字敲，既容易拼错也记不住全站有哪些模型/分组。

## 目标

把这两个字段升级为「可搜索的候选下拉」（combobox）：既能从全站候选里挑，也保留手动输入任意值的能力（方便查历史上出现过、但当前列表里已没有的模型/分组）。其余筛选逻辑、提交参数、URL 同步均不变。

## 关键决策

- **保留手动输入**：使用 combobox 的 `allowCustomValue`，而非纯 `Select`。候选只是提示，输入框仍可输入任意字符串，避免把"查历史值"的能力堵死。
- **候选取全站**：模型用 `getAllModels`（`GET /api/channel/models`），分组用 `getGroups`（`GET /api/group/`），都是全站列表，而非"当前用户可用"。
- **候选查询按管理员开关**：上述两个接口均为 `AdminAuth()` 管理员专用。因此候选查询用 `canManageScope`（`useIsAdmin()`）做 `useQuery` 的 `enabled`：管理员能拿到全站候选；非管理员不发这两个请求（避免 403），combobox 退化为纯手输框，功能不受影响。
- **不改提交契约**：选中/输入写回的仍是字符串，前端 `filters.model` / `filters.group` 与最终发给后端的 `model_name` / `group` 参数、URL search 同步逻辑全部保持原样。

## 实现

仅前端改动，无后端改动。

- `web/default/src/features/usage-logs/components/common-logs-filter-bar.tsx`
  - 新增两个 `useQuery`（`['log-filter-models']` / `['log-filter-groups']`，`staleTime` 5 分钟，`enabled: canManageScope`），分别调用 `getAllModels` / `getGroups`，映射为 `ComboboxInputOption[]`。
  - `modelFilter` / `groupFilter` 由 `LogsFilterInput` 改为 `Combobox`（`allowCustomValue`，`placeholder`/`searchPlaceholder` 用 `t('Model Name')` / `t('Group')`，空态 `emptyText={t('No option found.')}`）。
  - 数据源复用现成 API：`getAllModels` 自 `@/features/channels/api`，`getGroups` 自 `@/features/users/api`。
- i18n：新增 1 条文案 `No option found.`（空候选提示），en/zh/zh-TW/fr/ja/ru/vi 均已补齐并经 `bun run i18n:sync`；`Model Name` / `Group` 为既有键。

## 行为变化（注意）

- 原「分组」输入框在隐藏敏感信息时会做密码掩码（`type={sensitiveType}`）。combobox 的输入框是普通文本，无法掩码，且掩码与候选下拉互相矛盾，因此**分组字段不再跟随"隐藏敏感信息"做掩码显示**。模型名本就无掩码，不受影响。
- 未接「回车即搜索」：combobox 的回车用于选中候选，搜索走筛选栏右侧「搜索」按钮。

## 验证

- `bun run typecheck` 通过。
- 改动文件 `oxlint` 0 error / 0 warning（仓库存量 lint 问题与本次无关）。
- 各 locale JSON 可解析，`No option found.` 在 7 个语言文件均存在。
