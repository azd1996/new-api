# 集群 master/slave 拓扑说明

## 结论

当前 new-api 的 `master/slave` 是后台职责差异，不是严格的管理面/数据面隔离。所有实例默认都会注册管理接口和模型 Relay 接口，实例角色不会自动完成请求转发、读写分离或主从选举。

可以部署多个 master 做管理入口容灾，也可以让 slave 承担客户模型请求，但必须通过外部负载均衡、Ingress 或网络 ACL 划分流量。

## 推荐拓扑

```text
管理域名 / 管理 LB
    ├── master-a
    └── master-b

模型访问域名 / Relay LB
    ├── slave-1
    ├── slave-2
    └── slave-N

所有实例共享：
    ├── 主业务数据库
    ├── 日志数据库
    └── Redis
```

建议配置：

- master 节点设置 `NODE_TYPE=master`，只加入管理 LB。
- slave 节点设置 `NODE_TYPE=slave`，只加入模型访问 LB。
- 管理 LB 不转发到 slave。
- Relay LB 不转发到 master。
- 通过防火墙或 Ingress ACL 阻止 slave 暴露管理入口。
- master 和 slave 使用相同的主数据库、日志数据库和 Redis。

## 请求与日志行为

`NODE_TYPE` 不会阻止 master 处理模型请求，也不会阻止 slave 访问管理接口。所有实例都会注册 `/v1` Relay 路由和管理路由，因此流量隔离必须在应用外部完成。

消耗日志和错误日志由实际处理请求的实例直接写入共享 `LOG_DB`。slave 不会把日志转交给 master，master 查询日志时读取的也是共享日志数据库。这里的日志库与主业务库分工属于业务分库，不等同于数据库 primary/replica 读写分离。

## 多 master 的注意事项

多个 master 可以同时提供管理页面和日志查询，但当前没有应用层 leader election，不能视为自动互为主备。

需要特别注意：

- 多个 master 可能同时执行数据库 migration。发布或扩容时应单独安排 migration 步骤，避免多个 master 同时冷启动迁移。
- 部分 master-only 后台任务虽然有数据库租约或去重机制，但不是所有任务都保证跨实例互斥。应观察凭据刷新、订阅额度重置等任务是否重复执行。
- 管理接口在任意实例都可注册。若 slave 对外可达，不能只依赖 `NODE_TYPE` 保护管理 API。
- 各实例包含本地缓存，并通过定时同步传播配置。管理操作完成后，其他实例可能在一个同步周期内继续使用旧配置。
- `NODE_TYPE` 未设置、为空或拼写错误时会被当成 master，部署配置必须显式校验。

## 发布与故障切换建议

1. 先执行数据库 migration，并确认主库和日志库结构完成更新。
2. 再启动或滚动更新 master 节点，避免两个 master 同时抢迁移。
3. 最后更新 slave 节点，让客户流量始终有可用后端。
4. master-a 故障时，将管理 LB 切换到 master-b；这不会影响 slave 继续处理模型请求。
5. slave 故障时，由 Relay LB 摘除故障节点；master 不应作为 Relay 后备节点，除非明确接受 master 同时处理客户请求。

## 代码依据

- `common.IsMasterNode` 由 `NODE_TYPE != "slave"` 判定。
- `router/main.go` 对所有实例统一注册 API、Dashboard 和 Relay 路由。
- `model/main.go` 仅在 master 上执行 migration。
- `model/log.go` 将日志直接写入 `LOG_DB`。

