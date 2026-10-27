# 自动调度（Auto Scheduling）

> 状态：阶段一（配置 + 预览），**尚未接入真实请求路由**。
> 入口：系统设置 → 模型与路由 → 自动调度。
> 原始需求见 `docs/schedule-design.md`。

## 背景与目标

按**用户分组 / 用户级别 / Key** 为模型调用设置调度策略（优先调度 / 优先不调度 / 排除调度）。
判断依据来自 VictoriaMetrics（VM）时序指标（TTFT、缓存命中率等），通过 PromQL 查询得到。

阶段一只做：VM 数据源配置、PromQL 规则模板库、规则（对象 × 模型 → 模板 + 占位符参数）管理，
以及用真实数据源**预览** PromQL 结果。阶段一不影响任何请求的实际路由。

## 为什么查 VM 而不是查网关自身指标

网关进程内（`pkg/prom_metrics`）只有当前累计值的瞬时快照，没有历史采样、没有 PromQL 引擎，
且集群多实例各自只看到本机计数。而规则模板需要 `increase(...[12h])`、`topk`、`sort_desc`、
`on(...) group_left(...)` 这类跨时间、跨序列的运算，必须由 VM 聚合并执行 PromQL。
因此网关只负责**占位符替换 + 转发**到 VM 的 `/api/v1/query`，绝不自己实现 PromQL。

## 数据模型

三张表均以 `x_` 前缀命名，规避未来上游新增同名表的冲突（GORM `TableName()` 固定表名，
主库 AutoMigrate）。

- **`x_schedule_datasource`** — VM 数据源
  `id, name, base_url, username, password, enabled, timeout_seconds, created_time, updated_time`
  - `base_url` 存到服务根（如 `http://host/select/11/prometheus`），代码按需拼 `/api/v1/query`。
  - `password` 为 Basic Auth 凭据，`json:"-"` 不随接口下发；列表用 `password_set` 标识是否已配置。

- **`x_schedule_rule_template`** — 规则模板
  `id, name(唯一), promql, result_label, result_mode, description, created_time, updated_time`
  - `promql`：含 `__PLACEHOLDER__` 占位符的查询模板。
  - `result_label`：从查询结果里取哪个 label 作为被调度实体标识（如 `api_key_name`）。
  - `result_mode`：`rank`（结果按序作为优先级）或 `exclude`（结果集内对象踢出候选）。

- **`x_schedule_rule`** — 规则
  `id, target_type(group|user|token), target_value, model, template_id, params, enabled, created_time, updated_time`
  - `params`：占位符名（不含下划线，如 `MODEL`）→ 值的 JSON 字符串。
  - 结果（第六列）不落库，预览时实时查 VM 展示。

<!-- PLACEHOLDER -->
## 占位符与策略

模板 PromQL 用 `__[A-Z0-9_]+__` 作占位符（如 `__MODEL__ __API_KEY_NAME__ __TIMERANGE__ __TOPK__ __THRESHOLD__`）。
规则按模板扫描出的占位符动态填值；保存时序列化为 `params` JSON 字符串。

三种策略**由 PromQL 自身表达**，不单独建一层：
- 优先 / 优先不：排序方向（`sort_desc` / 反向），结果即优先级顺序 → 模板 `result_mode = rank`。
- 排除：阈值过滤（`... > __THRESHOLD__`），命中即踢出 → 模板 `result_mode = exclude`。

`result_mode` 是结果如何被（未来的）调度器使用的最小语义位：PromQL 只产出 `(label, value)` 行，
无法从结果形状反推是“按序优先”还是“命中即排除”，故放在模板上标注。

## 接口（均 `/api/schedule`，AdminAuth）

- 数据源：`GET/POST /datasource`、`PUT/DELETE /datasource/:id`、`POST /datasource/test`
- 模板：`GET/POST /template`、`PUT/DELETE /template/:id`
- 规则：`GET/POST /rule`、`PUT/DELETE /rule/:id`
- 预览：`POST /preview` — 传 `{rule_id}` 或 `{template_id, params}` 或 `{promql, result_label, params}`；
  可选 `datasource_id`（缺省取首个启用的数据源）。返回 `{resolved_promql, rows:[{label,value,labels}]}`。

数据源 `PUT` 传空 `password` 表示保留原密码。

## 预览链路（`service/x_schedule.go`）

1. `ExtractSchedulePlaceholders` 扫描占位符；`resolveSchedulePromQL` 替换，仍有未解析占位符则报错。
2. `queryScheduleDatasource` 向 `base_url + /api/v1/query` 发**瞬时查询**（POST form，Basic Auth，
   context + client 双超时），错误/成功分支响应体均用 `io.LimitReader` 限长。
3. 按 `result_label` 归约为 `(label, value)` 行返回。

## 安全

- 全部接口 AdminAuth；数据源密码落库但 `json:"-"` 不回传，前端只写不回显、列表仅显示“已配置/未设置”。
- `base_url` 为管理员可信配置、只读查询；响应体读取限长防异常上游致内存膨胀。

## 阶段边界与后续

- 阶段一：仅配置 + 预览，不接真实调度。“分组/账户/Key 依次覆盖生效”（key > user > group）
  属调度期（阶段二）解析优先级，当前仅把数据存住。
- 阶段二（规划）：把规则结果喂给调度决策、定期拉取 VM 刷新结果的后台任务。

## 相关文件

- 后端：`model/x_schedule.go`、`service/x_schedule.go`、`controller/x_schedule.go`、
  `router/api-router.go`、`model/main.go`（迁移登记）
- 前端：`web/default/src/features/system-settings/models/scheduling/`、
  `web/default/src/features/system-settings/models/section-registry.tsx`

