# 原始设计需求（你需要后续完善）

自动调度策略

为用户分组、用户级别设置模型调用的策略，策略包括优先调度、优先不调度、排除调度。用来决定优先或不优先的来自于ttft、命中率等指标这些来自于vm时序数据库，至于排除调度，可以是基于这些指标额外设置阈值。

前端设想

在newapi前端管理员下增加"调度"页面。在这个页面里，
1. 顶栏是配置的VM数据源；
2. 顶栏之下是规则模板列表，第一列是序号ID，第二列是规则模板名，第三列是规则模板，规则模板实际是promql语句，但是里边有占位符，可以替换字符串。

例如：
一个查询缓存命中率的promql原本长这样：
sum by (api_key_name, model) (
    increase(ai_gateway_cache_read_input_tokens_total{model=~"claude-opus-5-5"}[12h])
    * on(api_key) group_left(api_key_name)
    max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*客户名关键词.*"})
)
/
sum by (api_key_name, model) (
    increase(ai_gateway_channel_input_tokens_total{model=~"claude-opus-5-5"}[12h])
    * on(api_key) group_left(api_key_name)
    max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*客户名关键词.*"})
)

里边显然 model、api_key_name、还有时间范围12h都是可以被替换的。因此设置成占位符 __MODEL__ __API_KEY_NAME__ __TIMERANGE__，最终替换完之后，这个promql就可以用来查数据了。

sum by (api_key_name, model) (
    increase(ai_gateway_cache_read_input_tokens_total{model=~"__MODEL__"}[__TIMERANGE__])
    * on(api_key) group_left(api_key_name)
    max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*__API_KEY_NAME__.*"})
)
/
sum by (api_key_name, model) (
    increase(ai_gateway_channel_input_tokens_total{model=~"__MODEL__"}[__TIMERANGE__])
    * on(api_key) group_left(api_key_name)
    max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*__API_KEY_NAME__.*"})
)

那么进一步地，一个 缓存命中率优先 的规则模板就变成了：

sort_desc(
    sum by (api_key_name, model) (
        increase(ai_gateway_cache_read_input_tokens_total{model=~"__MODEL__"}[__TIMERANGE__])
        * on(api_key) group_left(api_key_name)
        max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*__API_KEY_NAME__.*"})
    )
    /
    sum by (api_key_name, model) (
        increase(ai_gateway_channel_input_tokens_total{model=~"__MODEL__"}[__TIMERANGE__])
        * on(api_key) group_left(api_key_name)
        max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*__API_KEY_NAME__.*"})
    )
)

如果为了避免调度时经过太多跳，可以限制若干跳，规则模板就写成：

topk(__TOPK__,
    sum by (api_key_name, model) (
        increase(ai_gateway_cache_read_input_tokens_total{model=~"__MODEL__"}[__TIMERANGE__])
        * on(api_key) group_left(api_key_name)
        max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*__API_KEY_NAME__.*"})
    )
    /
    sum by (api_key_name, model) (
        increase(ai_gateway_channel_input_tokens_total{model=~"__MODEL__"}[__TIMERANGE__])
        * on(api_key) group_left(api_key_name)
        max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*__API_KEY_NAME__.*"})
    )
)

如果约束缓存命中率低于某个阈值就不调度，规则模板就写成：

topk(__TOPK__,
    sum by (api_key_name, model) (
        increase(ai_gateway_cache_read_input_tokens_total{model=~"__MODEL__"}[__TIMERANGE__])
        * on(api_key) group_left(api_key_name)
        max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*__API_KEY_NAME__.*"})
    )
    /
    sum by (api_key_name, model) (
        increase(ai_gateway_channel_input_tokens_total{model=~"__MODEL__"}[__TIMERANGE__])
        * on(api_key) group_left(api_key_name)
        max by (api_key, api_key_name) (ai_gateway_api_key_info{api_key_name=~".*__API_KEY_NAME__.*"})
    ) > __THRESHOLD__
)

实际的promql语句可能有变化，但总体规则模板就是这么个意思。


第四列是规则模板计算完之后取的标签值，它必需是promql查询结果中的标签值


3. 在规则模板列表下边是规则列表，每一条规则分4列，第一列ID，第二列对象（可以是分组或者账户或者key，依次覆盖生效），第三列是模型，第四列是规则模板名，第五列是规则模板的占位符参数值，第六列是规则模板+占位符拉取之后得到的结果