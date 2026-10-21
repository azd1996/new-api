# 山西 master 经 NLB 不可达 / 集群流量倾斜 排查记录

## 背景与现象

- 集群:5 台 new-api,分布山西(阳泉 yq01,master 节点所在)与南京两机房,前置百度 NLB(四层 / FULLNAT),调用方来自北京机房。
- **现象 1**:实际只有南京机房有流量,山西节点长期无流量。
- **现象 2**:新建一个只指向山西 master 的 NLB 监听 `VIP:81` 后,办公电脑访问超时;而指向整个集群的 `VIP:80` 正常(能打开前端、正常使用)。

## 环境与拓扑

- 节点以 Docker 运行,`ai-gateway/newapi/docker/cluster-manager.sh` 管理;当前工程名 `newapicl`,网络 `newapicl_newapi-31525_network`,固定网桥 `br-newapi-31525`。
- new-api 容器内监听 `3000`,宿主按 `APP_PORT` 发布(节点 newapi-31525:宿主 `31525`→容器 `3000`,metrics `31526`)。
- 健康检查端点 `/api/status`(无需鉴权,`router/api-router.go:25`)。
- NLB 为 FULLNAT:后端看到的源 IP 是 NLB 节点段 `10.106.x.x`(非客户端真实 IP)。

## 排查过程(逐层排除)

1. 本机 `curl 127.0.0.1:31525/api/status` 返回 `{"success":true}` → **服务本身活着**。
2. NLB 控制台后端健康显示"正常" → 排除"服务挂了 / 健康探测打不通"。
3. `tcpdump -ni any port 31525`:来自 `10.106.x.x` 的连接**只有 SYN、无 SYN-ACK**,而 `127.0.0.1` 握手完整 → 内核收到外部 SYN 但不回包。
4. `nstat`:`IPReversePathFilter=0`、`InNoRoutes=0`、`ListenDrops` 停在 16 不涨 → 不是 rp_filter、不是缺路由、不是监听队列;SYN 在**到达 TCP 栈之前**就被丢(netfilter 层)。
5. `sysctl rp_filter=0`、`ip route get 10.106.x` 有回程路由、`ss` 显示 `docker-proxy` 监听 `:::31525` → 排除 rp_filter / 路由 / 绑定,锁定 **Docker iptables**。
6. `iptables -nxvL`:filter 表 `INPUT` ACCEPT 31525、`FORWARD` policy DROP 但 0 命中、`DOCKER-USER` 无 DROP → **filter 表没在丢包**。
7. `iptables -t nat -nxvL DOCKER`:发现 host `31525` 有**两条** DNAT——
   - `!br-d6bd5e0bf89e dpt:31525 → 198.18.0.2:3000`(排在前面,570 万包,吃掉全部流量)
   - `!br-newapi-31525 dpt:31525 → 198.18.4.5:3000`(当前活容器,0 包,被遮蔽)

## 根因

两个叠加的独立问题:

### 根因 1:陈旧 DNAT 遮蔽(机器侧)

- 旧 compose 工程遗留的空网络 `newapi_default`(`br-d6bd5e0bf89e`,subnet `198.18.0.0/24`)留下一条 `host:31525 → 198.18.0.2:3000` 的 DNAT。
- iptables 同链顺序匹配、DNAT 命中即止;这条排在正确规则前面,把外部 31525 流量全部转发到**早已删除的死容器 `198.18.0.2`** → 黑洞 → 有 SYN 无 SYN-ACK → 超时。
- 本机 `curl` 正常,是因为走 docker-proxy 用户态(指向当前活容器),不经这条 DNAT。
- 多次原地 `recreate` 只追加规则、不清旧的,叠加改用固定网桥名后的旧网络残留,最终形成遮蔽。
- `clean-iptables.sh` 原本清不掉它:既不是完全重复规则(dedup 不删),对应网络又仍在 `docker network ls`(不算孤立 bridge)。

### 根因 2:NLB 监听端口限制(NLB 侧)

- 机器侧修复后 `81` 仍不通,换 `8091` 即通 → 百度 NLB 对监听端口 `81` 有限制。

> 最初"只有南京有流量",本质就是山西 master 因根因 1 在 NLB 看来不可用 / 无正常应答,`80` 的流量只落到健康的南京节点。

## 修复

1. 增强 `clean-iptables.sh` 并执行清理,新增两段保守逻辑:
   - **过渡态保护**:容器处于 `restarting`/`created`/`removing` 时跳过,避免与 recreate/restart 竞态。
   - **陈旧 DNAT 清理**:删除 `--to-destination` 目标 IP 不属于任何现存容器(`docker ps -a`)的 DNAT;停止的容器不会误伤(Docker 停止时已撤规则、启动时会重写)。
   - 清理后 31525 DNAT 恢复指向 `198.18.4.5:3000`,计数开始增长。
2. 删除空壳网络 `docker network rm newapi_default`(`d6bd5e0bf89e`),连带清掉残留网桥与 filter 规则。
3. 对外监听改用 NLB 允许的端口段(`8091`)。

## 验证

- `iptables -t nat -nxvL DOCKER`:31525 只剩一条 → `198.18.4.5:3000`,计数增长。
- `iptables -nxvL | grep -c br-d6bd5e0bf89e` → `0`;`ip -br link show br-d6bd5e0bf89e` → 无输出。
- 抓包:`10.106.x.x` 的探测包已能回 `SYN-ACK`。
- 办公电脑经 NLB `8091` 访问正常。

## 补充:为什么 master 入站不通,控制台建渠道/建用户仍正常

- `common.IsMasterNode`(`common/init.go:90`)只控制后台任务(DB 迁移、权限 seed、定时任务等);**管理 API 所有节点都能处理**,写入**共享 MySQL**。
- 管理操作经 `VIP:80` 落到健康的南京节点并写入 DB;各节点按 `SyncFrequency` 从 DB 热加载(`SyncChannelCache`/`SyncOptions`/`authz.StartPolicySync`,`main.go:107/115/118`)。
- master 坏的只是**入站**;它容器本身活着、**出站连 MySQL/Redis 正常**,后台任务照跑。所以入站不通只造成**流量倾斜**,不影响管理写入与一致性。

## 预防

- 新增对外监听优先用 NLB 允许的端口段(如 `8xxx`),避开被限端口。
- 换工程名 / 重部署后用 `docker network prune` 清掉空壳网络,防止遗留 DNAT 遮蔽。
- 升级重建优先 `cluster-manager.sh down && up`(而非仅原地 `recreate`),让 docker-proxy 与 iptables 一致重建;必要时 `systemctl restart docker` 全量重建。
- 定期用改进后的 `clean-iptables.sh --dry-run` 巡检陈旧 / 重复规则。

## 附:关键命令

```bash
# 服务是否本机可达
curl -sS http://127.0.0.1:31525/api/status

# 抓包看外部 SYN 有无 SYN-ACK
tcpdump -ni any 'tcp port 31525'

# 排除 rp_filter / 路由 / 监听队列
nstat -az | grep -iE 'ListenDrops|ListenOverflows|InNoRoutes|IPReversePathFilter'
sysctl net.ipv4.conf.all.rp_filter
ip route get 10.106.214.3

# 看端口归属与 DNAT 目标
ss -tlnp | grep ':31525'
iptables -t nat -nxvL DOCKER

# 清理 + 删空壳网络
./clean-iptables.sh --dry-run && ./clean-iptables.sh
docker network inspect <id> --format '{{.Name}} containers={{json .Containers}}'
docker network rm <id>
```
