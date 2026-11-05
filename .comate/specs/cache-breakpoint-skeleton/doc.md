# Claude 缓存命中率：请求骨架存储 + 断点调整

> 状态（2026-11-05）：损失量统计、上一轮成功锚点保留及稳定基础前缀已有实现（§7.6）；客户编辑入口提供五项独立策略和模型前缀选择，含无缓存标记客户端兜底（§7.7–§7.8），客户列表提供统计入口。客户策略使用主库 TEXT 配置列，运行时锚点使用 Redis/进程内混合缓存（§7.10）。本次查询诊断新增 Claude 独立“推进 / 推进 1h”回放，并将断点覆盖不足与稳定前缀原因未明分开（§7.12）；§7.11 将哈希相同差额直接归上游的旧阶段口径已被纠正，当前顺序见 §7.6.4。不改变在线请求、数据库或计费。未配置客户继承原行为，显式配置覆盖旧规则，兜底默认关闭。模拟收益不是保证可回收量；真实 case 尚未联调。

## 1. 背景

- 客户极度在意缓存命中率，发现命中率低后会直接质疑网关；客户态度急，**不会配合再跑一段时间抓 DEBUG**。全量开 DEBUG 日志量和 IO 又太大。
- 触发案例（2026-10-30 分析）：某客户一小时命中率 43.4%。根因不是没打 `cache_control`，也不是前缀漂移，而是客户端一半请求（A 形态）把 messages 尾部断点打在了末尾那条每轮移位的 `<total_tokens>` 提醒消息上；正确位置是最后一个稳定的 assistant `tool_use` 块（C 形态的 n-3）。86 条 A 形态请求有 2779 万未缓存 token，修正后预计命中率可到 85% 以上。另外 TTL 全是 5m，轮次间隔几十秒到几分钟，空档期容易过期。

## 2. 目标

1. **事后可分析**：不依赖 DEBUG、不需要客户配合，基于常开记录的数据，回答"这个客户这段时间为什么没命中、损失多少、责任在谁"。
2. **断点调整**：检测到断点位置不合理时，网关自动把断点放到稳定位置。

非目标：不存、不看请求正文；不处理 OpenAI 格式转 Claude 的路径（只做原生 `/v1/messages`，即 `relay/claude_handler.go`）。

## 3. 现状

- 1h TTL 已有能力：`extended_cache_ttl_setting`（`relay/claude_handler.go:150-160`），按（分组, 模型）把已有断点改成 `ttl:1h` 并加 beta 头。只改 TTL，不新增、不移动断点。
- 统计分析浮窗（`model/log_analysis.go:242` `classifyCacheRow`）只能分出命中 / 冷写入 / 长度不足 / 其他未命中，断点错位、TTL 过期、前缀漂移都混在"其他未命中"里。
- 网关不保存请求体，日志里只有 usage 数字。
- param-override 只认固定 JSON 路径，表达不了"最后一个稳定 tool_use 块"这种按内容和位置的选择。

## 4. 整体结构

```
第一步  请求骨架存储（ClickHouse）  ──►  查询时归因 / 会话分叉比对 / 报表导出
第二步  断点调整（claude_handler 内实时改写）  ──►  效果由第一步的报表验证
```

先做骨架，采集基线；再对指定客户开断点调整，前后对比都在报表里。

## 5. 第一步：请求骨架

### 5.1 思路

保留请求的完整结构，把每个内容块替换成哈希。**不存任何正文字符**（不留预览）。只用哈希判断"前面的内容是否稳定"。归因不在请求时算死，而在查询时由骨架重算，归因规则以后可以改，历史数据照样能重算。

### 5.2 骨架内容

- **块的遍历顺序**：按 Anthropic 计算缓存前缀的顺序：`tools` → `system` → `messages[*].content[*]`。
- **块哈希**：用 xxHash64（`github.com/cespare/xxhash/v2`，仓库已有依赖，`relay/claude_handler.go` 已在用），直接对 gjson 取到的块原始字节求哈希，不做重新序列化，也不压缩空白。只有带 `cache_control` 的块（每条请求最多 4 个）先删掉 `cache_control` 字段再求哈希，这样挪动断点不会改变内容指纹。字节级的变化也算"变了"，宁可多报，不能漏判。
  - 风险：如果客户端每轮重新排列 JSON key 的顺序或改变格式，内容没变也会被判为"变了"。遇到这种情况，表现是"哈希变了但上游照样命中"，归因时能识别出这种矛盾，再针对处理。
  - 性能实测（2026-10-31，Apple M3 单核，`go test -bench`）：
    - 纯哈希吞吐：xxHash64 约 8 到 9.5GB/s，CRC32C 约 5.6GB/s，FNV-64a 约 0.53GB/s。
    - 完整骨架提取（gjson 遍历 + xxHash64）：290KB、179 块约 0.58ms；3MB、1219 块约 7.4ms。其中约 93% 的时间花在 gjson 遍历上，哈希本身不是瓶颈。
    - 集群峰值约 22 条/秒，按平均 0.6ms 算，全集群合计每秒约 13ms CPU，可以忽略。
  - 哈希位宽：64 位和 32 位速度相同，因为耗时取决于输入字节数，与输出长度无关。改用 32 位每块只省 4 字节；按每天约 2 亿次块比对算，每天约有 0.05 次误判为"相同"。存储并不紧张，所以保留 64 位。
  - 取舍结论（备选方案及不选的原因）：
    - **CRC32C**：有硬件加速，但实测比 xxHash64 慢约 40%，而且只有 32 位。
    - **FNV-64a**：逐字节计算，慢 15 倍以上。
    - **SHA-256 等密码学哈希**：这里不需要防篡改，只要能发现"内容变了"，额外的开销没有意义。
    - **抽样哈希**（只哈希长度加首尾若干字节）：易变内容经常在块中间（例如 tool_result 里被合并进去的 `Token usage: 150k/200k`），会漏判。骨架最不能出错的就是漏判，所以排除。
    - **省 CPU 的正确方向**：哈希只占骨架提取耗时的约 7%，大头在 gjson 遍历。真要优化，应该减少遍历次数（比如一次遍历同时拿到块边界和断点位置，避免对 `[]byte` 做额外的 string 拷贝），而不是换哈希算法。按当前量级，不需要优化。
- **与服务端缓存语义的关系**：Anthropic 和 Bedrock 先把请求解析、渲染成模型输入，再按 token 前缀缓存（tools → system → messages，从开头一直到带 `cache_control` 的那个块）。缓存的不是 JSON 字节，也不是两个断点之间的片段。骨架按块的原始字节求哈希，比服务端更严格：内容不变、只是 JSON 的格式或 key 顺序变了，服务端可能仍然命中，骨架却会报"变了"。反过来，只要骨架判定"没变"，服务端那边一定也没变，不会漏判。
- **原样保留的短字段**：model、thinking 配置、tool_choice、tool 名、tool_use 名、块类型、role、各断点的位置和 TTL、块的字节长度（用于估算损失 token）。
- **参数指纹**：对 model、tool_choice、thinking 配置、`output_config.effort` 单独算一个哈希。thinking 配置和 effort 会被渲染进模型输入，所以改了就等于改了前缀，Anthropic 文档说明它会让消息区缓存失效，有时连 system 和 tools 的缓存也会失效。model 不同则缓存本来就不共享。这一类需要和"前缀变化"分开归因。`max_tokens`、`temperature`、`stream` 这类只控制采样或输出方式的参数不会被渲染进输入，不放进指纹，以免误报。
- **lane**：`hash(model + tools + system 第一块)`。Claude Code 的主对话、子 agent 和 haiku 小调用共用 session_id，按 lane 区分，各自比对。
- **请求后补齐**：响应结束后补上 usage（`cache_read` / `cache_creation` / `input`）、渠道 ID、key 序号、节点名，再写入。
- **抓取时机**：最终出站请求，即上游实际收到的内容。网关做过断点调整的，用 `fixed` 字段标出。

### 5.2.1 计算与记录的时机（`relay/claude_handler.go` `ClaudeHelper`）

分三步，都在同一次 `ClaudeHelper` 调用内完成：

1. **计算骨架（同步，发请求前）**：放在所有改写之后、`NewOutboundJSONBody` 之前，也就是 `recordRequestSessionID` 那一处（约 `:170-181`）。执行顺序是：metadata 注入 → 断点调整 → 强制 1h TTL → param-override → **计算骨架** → 发请求。
   - 必须在这里算，有两个原因：这时拿到的就是上游最终收到的字节；而且 `:187` 之后 `jsonData` 会被置为 nil，交给 outbound body，之后就拿不到了。
   - 结果只保留哈希数组等元数据（几 KB），不保留请求体的引用，请求体照常释放。
   - 耗时：平均约 0.6ms，p99 约 7ms（§5.2 实测），对首字延迟的影响可以忽略，所以不需要为此开协程。
   - 同时记下请求开始时间 `ts`。
2. **补齐结果（同步，响应结束后）**：在 `DoResponse` 返回之后补上 usage、结束时间、渠道、key 序号、状态。流式请求要等流读完才能拿到 usage，`DoResponse` 返回时正好已经读完。
   - 用 `defer` 实现，这样上游报错（非 200）、`DoResponse` 出错、客户端断开等提前 return 的路径，也会记一行，`status` 标为 `error`，usage 为 0。
3. **写入（异步）**：把补齐后的一行投进有界队列就返回。后台协程批量写 ClickHouse（§5.3），队列满了直接丢弃并计数。

**各种路径的处理：**

- **透传路径**（`PassThroughRequestEnabled` / `PassThroughBodyEnabled`，`:93-98`）：出站请求就是客户端原始请求体，从 `common.GetBodyStorage` 读取字节来计算骨架。透传路径上不做断点调整，`fixed` 为空。
- **转 Responses 路径**（`textRequestViaResponses`，`:80-90`）：上游不是 Claude 格式，不计算骨架。
- **重试**：`controller/relay.go` 的重试循环每一轮都会重新进入 `ClaudeHelper`，所以每次尝试各记一行，用同一个 `request_id` 区分不同的 `attempt`。归因时只把 `status = ok` 的那一行当作"上一轮"。失败的尝试也保留，用来解释"换了渠道导致未命中"。
- **日志库不是 ClickHouse**：第 1 步直接跳过，不计算骨架（§8）。

**为此在表里补充的列：** `end_ts DateTime64(3)`（"并发未就绪"的判定要用上一轮的结束时间）、`status LowCardinality(String)`（`ok` / `error`）、`attempt UInt8`。

### 5.3 存储：单机 ClickHouse

选择理由（对比各节点本地文件）：

- 5 台节点的数据都写进同一张表，跨节点查询就是一条 SQL。不需要节点内网地址上报、按节点回源，也不依赖山西和南京两个机房互通。
- 同一会话相邻两轮约 95% 的块哈希相同，按会话排序后 ZSTD 能压缩重复部分。
- 按客户、按天聚合报表是 ClickHouse 擅长的。
- 建表时带 `TTL`，到期自动删除。

另一套是数据组包装的远程集群，只能走专用接口查询，不适合会话视图这种随点随查的场景。以后需要长期归档时再考虑接入。

表结构草案（参照 `model/audit_log.go:241` 对 ClickHouse 单独 `CREATE TABLE` 的做法）：

```sql
CREATE TABLE IF NOT EXISTS cache_skeletons (
  ts DateTime64(3), end_ts DateTime64(3), request_id String, attempt UInt8,
  status LowCardinality(String), node LowCardinality(String),
  token_id UInt32, session_id String, lane UInt64,
  channel_id UInt32, key_idx UInt16, model LowCardinality(String),
  param_fp UInt64,
  blk_hash  Array(UInt64)                 CODEC(ZSTD(3)),
  blk_kind  Array(LowCardinality(String)) CODEC(ZSTD(3)),  -- tool/system/user.text/assistant.tool_use/...
  blk_name  Array(LowCardinality(String)) CODEC(ZSTD(3)),  -- tools 定义名 / tool_use 名,其余为空;用于人读视图
  blk_len   Array(UInt32)                 CODEC(ZSTD(3)),
  msg_start Array(UInt16)                 CODEC(ZSTD(3)),  -- 每条消息的起始块序号
  bp_idx Array(UInt16), bp_ttl Array(LowCardinality(String)),
  fixed LowCardinality(String),
  cache_read UInt32, cache_creation UInt32, input UInt32,
  parent_request_id String, prefix_len UInt16               -- 预留：增量编码用，初版不写
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(ts)
ORDER BY (token_id, session_id, lane, ts)
TTL toDateTime(ts) + INTERVAL 14 DAY
SETTINGS ttl_only_drop_parts = 1
```

- 日志库不是 ClickHouse 的部署：这个功能直接关闭，不做 MySQL/PG/SQLite 版本（待确认，见 §8）。
- 写入：请求路径只把骨架投进有界队列。后台批量写入，每 1 到 5 秒或攒够 1000 行写一次。队列满了直接丢弃并计数，不阻塞转发。
- 保留：骨架保留 14 天。不存正文，因此原先"两级保留"里正文 48 小时那一级取消。

### 5.4 数据量评估（待实测）

- 每块原始约 14 字节（哈希 8 + 长度 4 + 类型约 1 + 消息起始约 1），每行固定字段约 150 字节。
- Claude Code 重度 agent 会话，每条请求平均 500 到 1000 块。
- 跨行压缩依赖后台合并，同一会话的行要挨在一起才能压缩，**不保证**。磁盘按最坏情况规划。

| 情况 | 每条请求 | 每天 100 万条、保留 14 天 |
|---|---|---|
| 不跨行压缩（最坏，平均 500 块） | 约 7KB | 约 100GB |
| 不跨行压缩（最坏，平均 1000 块） | 约 14KB | 约 200GB |
| 合并充分（预期） | 约 0.3 到 1KB | 约 4 到 14GB |

**评估步骤：**
1. 在单机 ClickHouse 上查过去 7 天每天的 Claude 请求量、现有表的大小、`system.disks` 剩余空间。
2. 上线简单版本（每行存完整骨架），先只对一个客户开一天，用下面的 SQL 看每行实际大小，同时观察 `parts` 数，判断合并是否跟得上：
   ```sql
   SELECT count() AS parts, sum(rows) AS rows,
     formatReadableSize(sum(data_compressed_bytes)) AS c,
     round(sum(data_compressed_bytes) / sum(rows)) AS bytes_per_row
   FROM system.parts WHERE active AND table = 'cache_skeletons';
   ```
3. 每行实际大小乘以全量日请求量，按合并前的原始大小留出余量。

**实测基线（2026-10-31，单机 ClickHouse 24.8.14，时区 UTC）：**

- 磁盘：`default` 盘 `/var/lib/clickhouse/`，总 902GiB，剩余 810GiB。按容量判断就是宿主机的 `/home`（nvme1n1）。
- 现有表：`logs` 有 1645 万行，压缩后 5.43GiB，压缩比 4.9，每行 354 字节。
- Claude 消费请求：完整日每天 53 万到 73 万条，其中约 84% 带缓存读写。峰值小时 78840 条（约 22 条/秒），平均每小时 22144 条。
- 单条请求总输入（prompt + 缓存读 + 缓存写）：p50 2.6 万、p90 19.5 万、p99 71.4 万 token，平均 7.7 万 token。

**按实测推算：**

- 块数：按 tools 加 system 约 2.5 万 token、约 50 块，之后平均每块 300 到 500 token 估算，平均每条请求约 150 到 400 块（p99 约 2000 块）。
- 每行大小，按最坏情况（不跨行压缩）算：150 + 14 × 块数 ≈ 2.3 到 5.8KB。
- 按每天 75 万条设计：每天 1.7 到 4.4GB，14 天 **24 到 61GB**。按每天 100 万条留余量：14 天 **32 到 81GB**。
- 剩余 810GiB，最坏情况下也只占 10% 以下。峰值写入约 22 条/秒 × 5.8KB ≈ 130KB/s，可以忽略。

**结论：** 按块存，保留 14 天，放在 `default` 盘，不需要任何减量手段，也不需要增量编码或 JBOD。建表时加上 `PARTITION BY toYYYYMMDD(ts)` 和 `SETTINGS ttl_only_drop_parts = 1`，到期时整个分区直接删除，不用重写数据。块数是按 token 推算的，试点上线后以 `bytes_per_row` 的实测值为准。

**放不下时的减量手段**（按推荐顺序）：
- 只存带 `cache_control` 的 Claude `/v1/messages` 请求（本来就是这个范围）。
- 按消息存累积哈希，不按块存。行数约减半，分叉点只能定位到"第几条消息"。
- 保留时长降到 7 天。
- 写入时做增量编码：每个 lane 在 Redis 里保存上一轮的哈希列表，只写 `parent_request_id`、共同前缀长度和新增块，每 50 轮写一个完整关键帧。每行约 150 到 300 字节，大小确定，不依赖合并。代价是又要依赖跨节点共享的 Redis 状态，查询时还要沿链拼接。
- 不做抽样：客户来质疑的那个会话，很可能恰好没被抽到。

### 5.5 查询时归因

同一 `(token_id, session_id, lane)` 按时间排序，相邻两轮比对，按顺序取第一个满足的原因：

1. **命中**：`cache_read > 0`。低于应命中的量时，标为部分命中。
2. **长度不足**：低于模型的最小可缓存长度，复用 `cache_min_tokens.go` 的阈值。属于结构原因。
3. **没有断点**：属于客户端原因。
4. **会话首轮**：没有上一轮，这次是正常的冷写入。
5. **参数变化**：`param_fp` 变了。属于客户端原因。
6. **前缀变化**：tools 或 system 区的哈希变了。属于客户端原因。
7. **断点错位**：上一轮最后一个断点的前缀在本轮已不存在，更早的前缀还在。属于客户端原因，即本次客户的 A 形态。
8. **并发未就绪**：本轮开始时，上一轮还没返回，缓存尚不可读。属于客户端的调用方式。
9. **TTL 过期**：前缀还在，但间隔超过了上一轮断点的 TTL。属于客户端配置。
10. **渠道切换**：前缀还在、也没过期，但渠道或 key 和上一轮不同。缓存按上游账号隔离，这一条是**网关原因**。
11. **上游未命中**：以上都不满足。属于上游原因。

- **应命中 token**：上一轮最后一个断点的前缀在本轮完整存在、且没过期时，本轮至少应读到上一轮的 `cache_read + cache_creation`。这个数来自上游返回的 usage，不需要分词器。
- **损失 token** = 应命中 − 实际 `cache_read`。
- **易变块识别**：按行为判断，不看内容。某个位置的块每轮哈希都变，下一轮就消失或换了位置，即为易变块。客户端换了提醒的写法也照样能识别。
- **实现选择**：单个会话在 Go 里逐轮比对；按客户或渠道汇总可以在 ClickHouse 里用 `lagInFrame` 取上一轮数据直接算，也可以分批拉到 Go 里算，实现时看性能再选。

### 5.6 呈现

- **统计分析浮窗**：新增"缓存未命中归因"表。每行一个原因，列出请求数、损失 token、占比、责任方，表头显示总命中率和可挽回 token。复用现有的 Markdown 和图片导出，可以直接发给客户。
- **会话视图**（现有"会话"Tab）：每一轮显示命中情况、原因、距上一轮的间隔、分叉点（例如"第 37 块变了，user.text，312 字节，上一轮的断点在这里"）。
- **日志 `other`**：只写单条请求内就能算出的字段：`n_bp`（断点个数）、`tail_volatile`（末尾断点是否在易变消息上）、`fixed`（网关是否改写过）。日志列表可以直接按这几个字段筛选。

### 5.7 多盘存储（单机 ClickHouse）

- ClickHouse 在服务端配置三层：disk（磁盘路径）→ volume（一组盘，组内按 part 轮流写入，即 JBOD）→ storage policy（多个 volume 按顺序排列，前热后冷）。表通过 `SETTINGS storage_policy = '...'` 选择策略。
- 当前结论：不需要多盘。骨架表放在 `default` 盘上（见 §5.4）。以后扩盘或把骨架表单独放到一块盘上的做法，见 §5.8。
- 网关侧预留：建表语句支持可配置的存储策略名（环境变量，默认为空，即使用 default 策略）。配置了策略时，追加 `SETTINGS storage_policy = '...'`。
- 风险：JBOD 没有冗余，坏一块盘就丢掉这块盘上的 part。骨架是 14 天的诊断数据，可以接受。日志表如果也要上多盘，要慎重考虑。
- 实际环境（2026-10-31，yq01-cdn-iaas-monitor01）：ClickHouse 是 `clickhouse/clickhouse-server:24.8` 容器（`newapicl_newapi-31525-clickhouse-1`），库名 `new_api_logs_newapi_31525`。数据盘都是 NVMe，没有机械盘：`/home`（nvme1n1，903G，已用 90G）、`/home/disk1`、`/home/disk2`（各 903G，空）。因此不需要冷热分层，最多把空闲 NVMe 做成 JBOD，或者给骨架表单独用一块盘。另外，容器里的数据目录挂载到宿主机的哪块盘，已确认：`/home/work/newapi-clusters/cluster/newapi-31525/volumes/clickhouse -> /var/lib/clickhouse`，落在 `/home`（nvme1n1）上。

### 5.8 以后扩盘 / 骨架表单独放一块盘

两件事难度都不大，在线就能做，只需要重启一次 ClickHouse 容器（几秒到几十秒）。

**前提：** ClickHouse 跑在容器里，每块新盘都要新增一个 bind mount，然后重建容器。这个挂载必须写进部署模板（`cluster-manager.sh` 使用的 compose 配置），否则下次重新部署时会丢失。

**通用步骤：**

1. 在宿主机上建目录，并改成容器内 clickhouse 用户的 uid（官方镜像是 101）：
   ```bash
   mkdir -p /home/disk1/clickhouse && chown -R 101:101 /home/disk1/clickhouse
   ```
2. 在 compose 中给 clickhouse 服务增加挂载：`/home/disk1/clickhouse:/var/lib/clickhouse-disk1`。另外挂载一个配置文件：`./clickhouse/storage.xml:/etc/clickhouse-server/config.d/storage.xml:ro`。
3. 按下面的场景 A 或 B 写 `storage.xml`。
4. 重建容器（`docker compose up -d clickhouse`），然后验证：
   ```sql
   SELECT name, path, formatReadableSize(free_space) free FROM system.disks;
   SELECT policy_name, volume_name, disks FROM system.storage_policies;
   ```
5. 重启期间网关写日志和骨架会失败：骨架队列直接丢弃，日志写入走现有的失败处理。选低峰期操作。

**场景 A：骨架表单独放到 disk1 上**

```xml
<clickhouse>
  <storage_configuration>
    <disks>
      <disk1><path>/var/lib/clickhouse-disk1/</path></disk1>
    </disks>
    <policies>
      <skeleton_disk1>
        <volumes>
          <main><disk>disk1</disk></main>
          <legacy><disk>default</disk></legacy>
        </volumes>
      </skeleton_disk1>
    </policies>
  </storage_configuration>
</clickhouse>
```

```sql
ALTER TABLE cache_skeletons MODIFY SETTING storage_policy = 'skeleton_disk1';
-- 可选：立即搬旧数据,否则等 14 天 TTL 自然清空。先列出还在 default 上的分区：
SELECT DISTINCT partition FROM system.parts
WHERE active AND table = 'cache_skeletons' AND disk_name = 'default';
-- 再逐个搬(表按天分区,分区值形如 20261031)：
ALTER TABLE cache_skeletons MOVE PARTITION 20261031 TO DISK 'disk1';
```

- ClickHouse 要求新策略必须包含旧策略用到的所有盘，所以 `default` 要作为 `legacy` volume 保留在策略里。新数据按 volume 顺序写入第一个 volume，也就是 disk1。
- 网关的环境变量配置同一个策略名之后，新环境建表时会直接带上这个策略。

**场景 B：给所有表扩容（JBOD）**

```xml
<policies>
  <jbod>
    <volumes>
      <main>
        <disk>default</disk><disk>disk1</disk><disk>disk2</disk>
        <load_balancing>least_used</load_balancing>
      </main>
    </volumes>
  </jbod>
</policies>
```

```sql
ALTER TABLE logs MODIFY SETTING storage_policy = 'jbod';
ALTER TABLE cache_skeletons MODIFY SETTING storage_policy = 'jbod';
```

- `least_used` 表示新 part 优先写到剩余空间最多的盘。已有的 part 不会自动搬走，需要时可以用 `MOVE PART` 手动调整。
- 网关每次启动都执行 `CREATE TABLE IF NOT EXISTS logs`，不会覆盖手动改过的 `storage_policy`。
- 风险：日志表是使用记录，用 JBOD 后坏一块盘会丢掉那块盘上的部分日志。要不要这样做需要单独评估。骨架表可以接受这个风险。
- 版本：当前 24.8 支持以上全部语法。

## 6. 第二步：断点调整

### 6.1 判定为错位

`<total_tokens>` 不是普通 user 消息里的一个 text 块，而是 `messages` 数组里一条**独立的 `role: "system"` 消息**（Anthropic 的 mid-conversation system message）。Claude Code 在每次 tool_result 后追加一条，内容形如 `<total_tokens>14992631 tokens left</total_tokens>`，数值每轮都变，并且客户端把消息区最后一个 `cache_control` 打在这条消息上。Anthropic 文档明确说明：按轮次清除的 system 消息（`clear_at`）不应带 `cache_control`，断点应放在前一个 user 轮的最后一个块上。来源：platform.claude.com/docs/en/build-with-claude/mid-conversation-system-messages，以及 Claude Code issue #88211（`totalTokensReminder`）。

消息区最后一个断点落在**最后一条消息**上，并且满足以下条件之一，即判定为可调整。`messages` 里的 `role: "system"` 消息**不一定易变**，例如 Claude Code 的 `# Environment` 消息会稳定地留在历史中；按 Anthropic 的建议，旧的提醒也应原地保留。真正易变的是"最后一条、下一轮就会被移走或改写"的那条消息：

- **带 `clear_at`**：Anthropic 文档明确说明这种消息永远不进缓存键，断点打在上面一定无效。判定可靠，允许新增断点，也允许挪动断点。
- **`role: "system"` 并且命中易变特征串**（默认 `["<total_tokens>"]`）：允许新增，也允许挪动。
- **`role: "system"` 但没有命中特征串**：只允许**新增**断点（在前一个块上），不挪动客户端原来的断点。万一这条 system 消息其实是稳定的，新增断点也没有坏处，只多占一个断点名额。
- **特征串命中，但出现在 user 文本里**：适用于把提醒塞进 user 消息的客户端，处理方式同第二条。

某个客户的 system 消息到底是不是易变，以骨架的跨轮比对为准：下一轮这个块消失或哈希变化，就是易变块。P1 上线后，根据实际观察结果补充特征串。

特征串只在内存里匹配，不落盘，和"不存正文"不冲突。

只有"断点打在易变的末尾消息上"这一类问题，网关能通过挪动断点修复。易变内容出现在历史消息中间（例如 `<system-reminder>` 里的动态值被合并进 tool_result、system 里带当前时间、tools 列表顺序不稳定），网关无法修复，只能由骨架归因为"前缀变化"，再反馈给客户端。

实现时要确认：`messages` 里的 `role: "system"` 消息和 `clear_at` 字段经过 `claude_handler` 后能原样转发给上游，不被 DTO 丢掉。

### 6.2 配置

新配置 `cache_breakpoint_fix_setting`，沿用 `GroupModelRule` 按（分组, 模型）匹配，和 `extended_cache_ttl_setting` 同级、同一个编辑界面：

```json
{
  "rules": [{"group": "cust-x", "models": ["claude-sonnet-5-5"]}],
  "volatile_patterns": ["<total_tokens>"],
  "strategy": "add_then_move"
}
```

### 6.3 改写规则

1. **找目标块**：从易变消息往前找，优先选它前面那条 user 消息的最后一个块（通常是 `tool_result`，也是 Anthropic 文档推荐的位置）；找不到时再选最近的 assistant `tool_use` 块。tool_result 位置靠后、本身也是稳定的，缓存覆盖的前缀比 tool_use（即原分析里的 n-3 位置）多一条消息。跳过 thinking、redacted_thinking 和空 text 块，Anthropic 不允许在这些块上打断点。
2. **`add_then_move`**：
   - 断点总数小于 4 时，在目标块上新增一个断点，客户端原有的断点保留。
   - 断点已满 4 个时，把末尾断点挪到目标块上。
   - 目标块上已有断点时，不做任何改动。
3. **TTL**：新断点沿用原断点的 TTL。
4. **记录**：在日志和骨架里标记 `fixed: added` 或 `fixed: moved`。

### 6.4 执行顺序（`claude_handler.go`）

metadata 注入 → **断点调整（新增）** → 强制 1h TTL（已有）→ param-override → 计算骨架 → 发请求。

断点调整放在强制 TTL 之前，新断点才能一起改成 1h。param-override 是人工显式配置，放在最后，优先级最高。

### 6.5 计费影响

不改计费代码，仍按上游返回的 usage 结算，但客户的实际花费会变化：

- 命中变多，读缓存按约 0.1 倍计价，客户成本下降。
- 同时开 1h TTL 时，写缓存从 1.25 倍升到 2 倍。

上线前需要和客户沟通。

## 7. 实施顺序

1. **P1 骨架**：
   - 在 `claude_handler.go` 计算骨架。
   - 异步批量写入 ClickHouse，建表。
   - `other` 写入三个轻量字段。
   - 先只对一个客户试点，实测数据量。
2. **P2 归因与呈现**：查询接口、统计分析浮窗的归因表、会话视图的逐轮归因和分叉点、导出。
3. **P3 断点调整**：等 P1 采到基线数据后，再对指定客户开启，用 P2 报表对比纠正前后的效果。

验证：
- 后端改动跑 gofmt 和 `go test`。断点调整的单测扩展现有的 `relay/claude_handler_cache_ttl_test.go`，表驱动覆盖 A/C 形态、断点已满 4 个、前面只有 thinking 块、目标块已有断点、未命中规则不改写。
- 前端跑 `bun run build`，新文案只加 en/zh。

## 7.1 P1 实施记录（2026-10-31，未提交）

已实现：记录请求骨架，并在日志详情里点按钮查看。

- 后端
  - `model/cache_skeleton.go`：`CacheSkeleton` 模型；`MigrateCacheSkeletons`（只在日志库为 ClickHouse 时建表，挂在 `migrateClickHouseLogDB` 里，由 master 节点执行）；有界队列（4096）加后台批量写入（每 500 行或 2 秒写一次，队列满时丢弃并计数）；`GetCacheSkeletons(request_id)`。
  - 环境变量：`CACHE_SKELETON_ENABLED`（默认 true，日志库不是 ClickHouse 时始终关闭）、`CACHE_SKELETON_TTL_DAYS`（默认 14）、`CACHE_SKELETON_STORAGE_POLICY`（默认为空）。
  - `relay/cache_skeleton.go`：`buildClaudeCacheSkeleton`（gjson 遍历 + xxHash64，带 `cache_control` 的块先删掉该字段再哈希；处理 tools、system 字符串或数组、messages 字符串或数组、顶层自动缓存断点、参数指纹、lane）；`finishClaudeCacheSkeleton`（补 usage、状态、渠道、key、session、attempt；Anthropic 语义的 input 不含缓存，OpenAI 语义的扣掉缓存部分）。
  - `relay/claude_handler.go`：用 `defer` 收尾，覆盖成功和失败的尝试；非透传路径在 `recordRequestSessionID` 之后计算骨架，透传路径从 `GetBodyStorage` 读字节计算；转 Responses 路径不记录。
  - `GET /api/log/cache-skeleton?request_id=`（AdminAuth），块哈希以 16 位十六进制字符串返回，避免 JS 精度丢失。
- 前端
  - 日志详情：管理员、消费或错误日志、Claude 模型时，显示"请求骨架 · 查看"按钮。
  - `cache-skeleton-dialog.tsx`：按 tools / system / 每条消息分组，展示块类型、名称、哈希前 8 位、大小、累计字节、断点 TTL；断点打在最后一条 system 消息上时标 ⚠。有多次尝试时逐个展示。
  - 新文案只加 en / zh。
- 未实现（留给后续）：日志 `other` 里的三个轻量字段（`n_bp` / `tail_volatile` / `fixed`）、跨轮对比视图、归因报表、断点调整。

验证结果：

- `go build ./...`、`go vet ./relay ./model ./controller ./router`：通过。
- `go test ./relay ./controller ./model -count=1`：通过。新增 `relay/cache_skeleton_test.go` 4 个用例，覆盖结构、移动断点后前缀哈希不变、参数指纹、顶层自动缓存。
- 前端：`vitest`（`lib/__tests__/cache-skeleton.test.ts`，4 个用例）、`tsgo -b`、`oxlint` / `oxfmt --check`（只检查本次改动的文件）、`bun run build`：均通过。
- **ClickHouse 实库未验证**：本机没有 ClickHouse。已提供 `model/cache_skeleton_test.go`，设置 `TEST_CLICKHOUSE_DSN` 指向一个临时库后运行，会验证建表幂等、数组列批量写入和读回。上线前需要先跑一次这个测试，或在试点环境确认 `system.parts` 里有 `cache_skeletons` 的数据。
- **2026-11-01 线上修复**：点"查看"报 `unsupported data type: &[]: Table not set`。原因是数组字段没写 `type:`，GORM 把 `[]string` / `[]uint64` 当成关联解析失败，查询和批量写入都会失败。已给 7 个数组列加 `type:Array(...)` 标签，新增 `TestCacheSkeletonSchemaParses`（去掉标签时能复现同样的报错）。

## 7.2 P2 会话缓存诊断（2026-11-01，未提交）

已实现：会话列表展开行顶部的"缓存诊断"，管理员可见。

- 推算（`model/cache_skeleton_analysis.go`）
  - 按 `session_id` 取骨架（按 ts 升序，上限 2000 行，超出标 `truncated`），再按 lane 分组，主会话和 subagent 分开推算。
  - 每个 lane 跑 4 个缓存模拟：客户端断点原样、忽略 TTL、网关调整后、调整后 + 1h TTL。缓存键是从 `param_fp` 起的滚动前缀哈希；条目在写入请求的 `end_ts` 之后才可读，按 TTL 过期，读取时续期；断点往前回看 20 个块；累计 token 低于 `cacheMinTokensFor(model)` 的断点不写缓存。
  - token 按字节占比估算：本轮真实输入总量（read + creation + input）按每块字节数分摊。
  - 调整模拟：最后一条消息是 mid-conversation system 且带断点时，在它前一个非 thinking 块上加一个断点（已有 4 个时改为移动）。
  - 每轮归因（先命中先返回）：error、first_turn、too_small、no_breakpoint、param_changed、prefix_changed（分叉点落在 tools/system）、concurrent；之后在 upstream（channel_switch / upstream_miss）、ttl_expired、breakpoint_misplaced 三项里取损失最大者，损失须超过本轮 5%，否则为 hit。
  - 汇总只算成功轮：actual = 实际 cache_read；current / adjusted / adjusted_1h = max(模拟值, 实际值)。
- 接口：`GET /api/log/cache-skeleton/session?session_id=`（AdminAuth），返回 `{enabled, lanes, summary, truncated}`；日志库不是 ClickHouse 时 `enabled=false`，前端不渲染面板。
- 前端（`session-cache-diagnosis.tsx`）
  - 汇总行：实际命中率 / 客户端断点上限 / 断点调整后 / 调整 + 1h TTL，以及三段损失（上游损失、断点位置不当、TTL 过期，单位 token）。
  - "展开轮次"后逐轮列表：时间、与上一轮结束的间隔、前缀条（灰 = 与上一轮相同，红 = 变化，绿 = 新增，竖线 = 断点位置，按 lane 内最大请求缩放）、实际与预期命中率、归因标签、⚠ 易变尾部断点、✗ 分叉块序号；"骨架"按钮打开 P1 的骨架弹窗。
  - 多个 lane 时每个 lane 单独显示一行汇总。
- 未实现：统计分析浮窗里的跨会话归因报表、`other` 轻量字段、断点调整本身（§6）。

验证结果：

- `go build ./...`、`go vet ./model ./controller ./router ./relay`、`go test ./model ./relay ./controller`：通过。`model/cache_skeleton_test.go` 新增 `TestAnalyzeCacheSkeletons` 4 个子用例：尾部 system 断点错位（current 0，adjusted 17647，归因 breakpoint_misplaced）、间隔 10 分钟（ttl_expired，1h 档恢复 18000）、换渠道后未命中（channel_switch）与下一轮命中（hit）、lane 分离。
- 前端：`vitest`（`lib/__tests__/cache-skeleton.test.ts` 共 9 个用例）、`tsgo -b`、`oxlint`、`oxfmt`（只针对改动文件）、`bun run build`：通过。`vitest run src/features/usage-logs` 有 17 个失败（group-filter / billing-source-visibility / mobile-filter / audit viewer），在干净的 HEAD worktree 上同样失败，与本次改动无关。
- 推算逻辑只在单测数据上验证过，尚未用真实会话的骨架核对。

## 7.3 修复：兜底会话 ID 每轮都变（2026-11-01，未提交）

现象：会话视图里 `content-fallback` 来源的会话几乎都只有 1 个请求，但缓存命中率很高（例：`efdea6306a5c6773`，1 个请求、缓存读 37,589、命中率 88.4%）。说明一个真实会话被拆成了多个单请求会话，上游缓存不受影响。

原因：客户端没带 `metadata.user_id` 和 `x-claude-code-session-id` 时，会话种子的"对话身份"部分取自 `claudeCacheControlSeed`，即所有带 `cache_control: ephemeral` 的块文本，包括 messages 里的块。客户端每轮都把消息断点挪到最新那条消息上，种子随之变化，每轮都得到新的 session_id。这个 session_id 同时写进注入给上游的 `metadata.user_id`，所以注入 session_id 想稳定会话的目的在这条路径上也没达到。

修改（`relay/claude_handler.go`）：

- 种子仍是 `model + 稳定 system 前缀 + 对话身份`，只改对话身份的取法，优先级如下：
  1. `x-claude-code-session-id` 头（不变）。
  2. **API key + 第一条消息内容**（`claudeFirstMessageSeed`，替代 `claudeCacheControlSeed`）。text 块取文本，其余块取原始 JSON 并去掉 `cache_control`，断点挪到或挪离第一条消息时种子不变。
  3. IP + UA + API key（不变，只在没有消息内容时用）。
- 已知代价：同一个 API key 下，model、system 和第一条消息都相同的两个对话会合并成一个会话（例如脚本批量跑同一个 prompt）。
- 上线后，同一个兜底会话注入上游的 session_id 也会在多轮之间保持不变。历史日志里的 session_id 不会重算。

验证：

- 先在 `relay/claude_handler_metadata_test.go` 加用例"消息断点后移"，在旧逻辑上断言两轮 session 不同，测试通过，确认问题可复现。
- 改逻辑后把断言改为两轮相同；另外把"cache_control 内容兜底"用例改成"第一条消息锚定"（含非 text 块去 cache_control），新增"第一条消息不同则会话不同""没有消息内容时走 IP+UA+key"。`go test ./relay -run 'TestClaudeSessionDerivation|TestFillClaudeMetadataUserID|TestClaudeHelperRecordsFinalSession'` 通过，`gofmt`、`go vet ./relay` 通过。
- `go test ./relay` 全量有 1 个失败：`TestBuildOpenAIChatCacheSkeleton`（多出一个 `assistant.` 块）。它来自工作区里同时在改的 OpenAI 骨架代码（`relay/cache_skeleton*.go`），与本修改无关。

## 7.4 OpenAI（GPT）骨架与诊断（2026-11-01，未提交）

用户上游主要是 Azure OpenAI（渠道类型为 OpenAI）。Chat Completions 和 Responses 两个入口都记录，Responses WebSocket 不覆盖。

- 缓存模型（来源：OpenAI Prompt Caching 201、Azure Q&A）：没有断点，1024 token 起自动缓存最长相同前缀，按 128 token 递增；`prompt_cache_key` 影响路由；默认在内存里保留 5–10 分钟；`prompt_cache_retention: "24h"` 仅 OpenAI 官方支持，**Azure 不支持，传了会报错**。
- 记录（`relay/cache_skeleton.go`）：
  - Chat Completions：`compatible_handler.go` 的直连路径（透传与非透传）。转 Responses 的路径在 `relayResponsesRequest` 里按上游实际收到的 Responses 体记录；Claude 转 Responses 的请求也走这里。
  - Responses：`responses_handler.go`，compact 请求不记录。
  - 块：tools → instructions → 结构化输出 schema → 每条 message 的内容块、tool_call / function_call（记工具名）、function_call_output、reasoning。`param_fp` = 模型 + tool_choice + reasoning effort。
  - 新列 `provider`（旧行默认 claude）、`cache_key_hash`、`retention`；`parent_request_id` 存 `previous_response_id`。`MigrateCacheSkeletons` 用 `ADD COLUMN IF NOT EXISTS` 幂等补列。
- 诊断（`model/cache_skeleton_analysis.go` 的 `openAILaneSim`）：每轮在每个块边界写缓存，读取取最长命中前缀，命中 token 按 1024 起、128 取整；内存保留按 10 分钟上限、读取续期。三档：
  - 前缀匹配上限：缓存按 prompt_cache_key 分区，key 一变就读不到；
  - 统一 prompt_cache_key 后：去掉分区；
  - 统一 key + 24h 保留：再把保留改成 24h（Azure 上只作参考）。
- OpenAI 归因：沿用首轮 / 低于 1024 / 参数变化 / 工具或 instructions 变化 / 并发 / 渠道切换 / 上游未命中，新增 `cache_key_changed`、`previous_response`（带 previous_response_id 的轮次前缀在上游，不推算，按实际计入）。
- 前端：详情页所有消耗/错误日志都显示"请求骨架"入口；OpenAI 骨架弹窗显示 key 哈希前 8 位、retention、previous_response_id；会话诊断按 provider 切换三档与损失项文案，逐轮标"prompt_cache_key 变化 / 未带 prompt_cache_key"。

验证：`go build ./...`、`go vet`、`go test ./model ./relay ./controller` 通过（新增 `TestAnalyzeOpenAICacheSkeletons` 4 个子用例、`TestBuildOpenAIChatCacheSkeleton`、`TestBuildOpenAIResponsesCacheSkeleton`）；前端 vitest 10 个用例、tsgo、oxlint、oxfmt、`bun run build` 通过。ClickHouse 补列与 Azure 真实流量均未验证。

## 7.5 断点调整、clear_at 修复、prompt_cache_key 注入（2026-11-01，未提交）

- **clear_at 丢失（已复现并修复）**：`relaykit/dto/claude.go` 的 `ClaudeMessage` 没有 `clear_at` 字段，非透传路径重新序列化后被丢掉；新增 `TestClaudeRequestKeepsMessageClearAt` 先失败、加字段后通过。官方要求（platform.claude.com mid-conversation-system-messages）：`clear_at` 只能是 `next_user_message` / `never`；需要 beta `mid-conversation-system-clear-at-2026-08-21`，否则报未知字段；turn-scoped 消息不允许带 `cache_control`。因此 `prepareClaudeTurnScopedMessages` 在带 clear_at 时自动补 beta 头（Bedrock 由 aws 适配器转成 body 的 `anthropic_beta`），并删掉 `next_user_message` 消息里的 `cache_control`。**行为变化**：此前 clear_at 被静默丢弃、请求能过；修复后会原样发给上游，上游或区域不支持该 beta 时会报错。
- **断点调整**（`relay/claude_cache_breakpoint.go`，配置 `cache_breakpoint_fix_setting`，按（分组, 模型）开启，默认关闭）：
  - 末尾是 system 消息且其中有断点：目标为前一条消息最后一个可打断点的块（跳过 thinking / redacted_thinking / 空 text），找不到再看再前一条；字符串 content 会改成一个 text 块。
  - 命中 `<total_tokens>` 或 turn-scoped：断点不满 4 个时新增，满了则挪；turn-scoped 一律挪（上游不允许留在原处）。没命中特征串的 system 尾部：只在有空位时新增。
  - 提醒附在 user 消息里（命中特征串）：目标是同一条消息里它前面的块。
  - 目标块已有断点时不改；新断点沿用尾部断点的 type/TTL；执行在强制 1h TTL 之前；结果写入骨架 `fixed`（added / moved）。
  - 未做：消耗日志 `other` 里的标记（骨架弹窗的"网关改写"一行已能看到）；特征串暂为代码常量，未做成配置项。
- **prompt_cache_key 注入**（`relay/openai_prompt_cache_key.go`，配置 `prompt_cache_key_injection_setting`，按（分组, 客户端模型）开启，默认关闭）：客户端没带 key 时生成 `gw-` + 16 位哈希，种子为 模型 + API key + 会话锚点；Responses 的锚点优先用 `client_metadata.session_id`，否则用 instructions + 第一条 input；Chat 用前两条消息。会话 ID 来源记为 `gateway.prompt_cache_key`。透传路径不注入。
- 前端：系统设置 → System Behavior 新增"修正错位的缓存断点""注入 prompt_cache_key"两个规则编辑器。

验证：`relaykit` 下 `GOWORK=off go build ./...`、`go test ./dto` 通过；根模块 `go build ./...`、`go vet`、`go test ./relay ./model ./setting/... ./controller` 通过（新增 `TestFixClaudeCacheBreakpoints` 8 个子用例、`TestPrepareClaudeTurnScopedMessages`、`TestDeriveOpenAIPromptCacheKey`）；前端 tsgo、oxlint、oxfmt、`bun run build` 通过；`vitest src/features/system-settings` 有 2 个文件因 `CHANGELOG.md?raw` 被拒绝导入而无法加载（与此前 HEAD 上同样的环境问题），其余 243 个用例通过。未在真实上游验证：Anthropic / Bedrock 对 clear_at 的接受情况、Azure 对 prompt_cache_key 的接受情况。

## 7.6 网关优化路线与阶段 1 损失统计（2026-11-02，已实现查询时统计与成功锚点保留）

### 7.6.1 网关优化路线与本阶段启用项

以下是网关侧优化路线；已实现第 1 项及第 2 项中的基础前缀检查点，其余仍保留为后续路线：

1. **已启用：保留上一轮成功缓存锚点**：让下一轮仍能找到此前已成功写入且前缀未变的锚点，而非只把断点机械地移到最新尾部。运行时按 session、渠道、Key、模型和 lane 隔离，在成功响应产生缓存写入后记录最终断点；下一轮仅在对应块哈希仍一致时恢复，并遵守最多 4 个断点限制。状态存储已迁移到 Redis/进程内混合缓存（§7.10），Redis 启用时跨实例、跨进程重启可见，按断点 TTL 过期；Redis 未启用时退回进程内短期状态，进程重启后不保留。需要保证锚点处于可查找窗口；当前模拟把窗口近似为包含断点自身在内的 20 个原始块位置，即 `bp-19 … bp`。这不是“前面 20 块再加断点”，也不是实际供应商缓存内部位置的精确复刻：并行 tool 调用等内容可能被供应商组合成不同的块，Anthropic、Bedrock 及其他代理的分组和回看行为需分别核实。
2. **基础前缀检查点已实现**：复用 `ShouldFixCacheBreakpoints(group, model)` 开关，在易变尾部修复之后、强制 TTL 之前，优先为首个合法且不含已知易变标记的 system 块添加断点；字符串 system 等价转换成单个 text 块。没有合适 system 时，保护 tools 的最后一个定义。已有目标断点保持原样，满 4 个槽位时跳过，顶层自动缓存也预留一个槽位，不驱逐客户端断点。新增断点默认 5m；若已有 1h 断点则使用 1h，避免新增早期 5m 断点破坏 TTL 顺序。基础前缀仍需达到供应商最小缓存 token 阈值才能受益，且首个 system 块只是保守候选，不保证业务上永远不变。验证：`gofmt`、`go test ./relay ./model -count=1` 通过；真实上游命中收益尚未验证。
3. **无断点兜底**：确实没有有效缓存断点时，再考虑在最后一个合法、非空且非 thinking / redacted_thinking 的块上加断点。先区分顶层自动缓存与真正未配置，不能仅凭旧骨架 `bp_idx` 为空就改写请求。
4. **真正的上游缓存域亲和性**：按供应商实际共享缓存的账号、组织、项目、region、部署或路由域保持亲和，不能把“同渠道/key”直接等同于同缓存域，也不能把“换 key”直接等同于缓存域变化；多个 key 可能共享域，同一 key 也可能被供应商再次路由。稳定 `metadata.user_id` 仅可能帮助供应商自身的路由，不是 Anthropic 的缓存键，也不保证缓存共享或命中。
5. **稳定网关出站输出**：避免每轮不一致的 tools 排序、system 注入、参数覆盖或历史重写。优化重点是稳定最终出站表示，不为追求命中率改写用户语义，不擅自删除动态内容、改 effort/thinking 或重写旧历史。
6. **可选冷并发等待**：仅在同一可复用前缀的首次写入尚未就绪时，评估有界、可取消的等待/合并策略，并保留超时、失败和延迟保护。此项存在首字延迟代价，不默认串行化整个会话，本阶段不实现等待。

### 7.6.2 阶段 1 的批准范围与数据口径

- **查询统计范围**：在现有会话详情诊断及“匹配筛选的会话”汇总中增加损失分解，复用已有骨架和回放；不修改 schema、采集格式、SQL/查询条件或扫描上限。
- **运行时锚点范围**：实现 §7.6.1 第 1 项的成功写入锚点保留及第 2 项的基础前缀检查点；不启用消息尾部无断点兜底、亲和路由或并发等待。基础前缀检查点复用原有断点修复开关，强制 TTL 仍按原配置生效。锚点写入依据是成功响应中的 `cache_creation` 与最终请求断点，属于网关侧可复用推断，不是上游提供的逐断点确认。
- 当前实现语义以 `model/cache_skeleton_loss.go` 的 `skeletonTurnLosses` 和 `model/cache_skeleton_analysis.go` 的回放、汇总为依据。阶段 1 只对成功、可回放的 Claude 尝试统计：`status=ok`、总输入非零且有块哈希；错误尝试不参与统计，也不作为下一次比较的上一轮。这里的“轮”是记录的 attempt，不是去重后的用户请求。OpenAI 保留既有诊断，不纳入本阶段的 Claude `loss_summary`；混合供应商时不能拿整体 `summary` 当成 Claude 损失分母。
- **已有诊断历史行可直接重算**，不必等新采集数据；没有骨架的请求无法补算。附录 D 所说“历史会话不显示”仅指骨架采集上线前未记录的数据，不是本阶段升级前已经保存的骨架。
- **筛选用于选择会话，不用于裁掉会话内轮次**：沿用 `logSessionQuery` 选中匹配筛选的 session_id，再取这些会话仍在保留期内的完整骨架生命周期。包含筛选请求时间之外的已保留轮次，不是“只统计时间窗内请求”，更不是“完整真实会话永久历史”。详情按 session_id 回放，跨会话汇总独立于列表分页，切换页码不改变统计总体。
- 沿用现有限制：单会话最多 2000 行；范围汇总最多 10000 个会话、总扫描 500000 行，并对每会话再限 2000 行。会话选择按最近活动优先，骨架按会话/时间排序后截断，**不是随机样本**；任一上限触发都应显示 `truncated` 的截断/样本警告，不能声称覆盖所有匹配历史。
- 即使 `truncated=false`，也不能证明骨架覆盖完整：保留期过期、队列丢弃、采集关闭和原本未记录的请求仍可能缺失。目前没有全量请求对照得出的骨架覆盖率，应标明覆盖未知；无可分析骨架不等于没有损失。

### 7.6.3 返回字段与守恒关系

在现有诊断响应上补充 `loss_summary`（lane、会话详情、匹配会话汇总），不替换原有 `summary` / `reason`。其字段为：

| 字段 | 含义 |
|---|---|
| `analyzed_turns` | 纳入分析的成功、可回放 Claude 尝试数，含未缓存量为 0 的尝试 |
| `total_tokens` | 这些尝试的总输入之和，每轮为 `cache_read + cache_creation + input` |
| `actual_read` | 这些尝试的上游实际缓存读取 token 之和 |
| `uncached_tokens` | 每轮 `max(0, total_tokens - actual_read)` 之和，包含缓存创建和普通输入，不仅是原始 usage 的 `input` |
| `items` | 按原因汇总的 `{reason, tokens, turns}`；`tokens` 为分配量，`turns` 为在该原因下有正分配量的受影响尝试数 |

逐轮返回 `losses`，元素同样为 `{reason, tokens, turns}`，每个非零项的 `turns=1`；没有未缓存输入时可省略。lane → 会话 → 匹配会话汇总按原因相加。

**token 数量是不重叠分区**：每轮及汇总均满足 `sum(items.tokens) = uncached_tokens`（逐轮对 `losses` 求和）。同一尝试可以贡献不同、不重叠的 token 给多个原因，所以各项受影响 `turns` 可以重叠，不能相加当请求总数。正常首轮冷输入、新内容和不可分类余量也在分区中，不等于全部都是可挽回缓存损失，更不等于金额损失。

### 7.6.4 逐轮分解顺序（与主原因标签分开）

先计算本轮未缓存量；为 0 则无分解项。以下按 lane 内上一次成功、可回放的尝试比较，采用固定顺序的增量瀑布分配，而不是先选一个标签再把整轮输入算给它：

1. **`first_turn`**：没有上一轮时，全部未缓存量归此项，并立即结束。含义是本次数据中“首个被观察到的可回放轮次”（bootstrap-first），不证明它是实际真实冷启动；保留期之前、跨 lane 或未记录的调用可能已经预热。
2. **`too_small`**：非首个观察轮次且本轮总输入低于模型最小缓存长度时，全部未缓存量归此项，并立即结束。首轮优先于长度不足。
3. 其余轮次以 `covered = actual_read` 起步，依次比较下列**累计前缀上限**。每项仅分配 `max(0, min(total_tokens, ceiling) - covered)`，有正增量才推进 `covered`；后一个上限不比已覆盖量高时不记量。这样实际命中始终是下限，局部命中不会重复归因。

| 顺序 | 比较的上限/反事实 | 分配原因及解释 |
|---|---|---|
| 1 | 当前客户端断点模拟 `current` | `channel_switch`（本轮渠道或 key 序号相对上一轮改变）或 `upstream_miss`（未改变但模拟可读、实际上游未返回）；前者仅表示切换相关，后者表示模型尚不能解释的差额 |
| 2 | 易变尾部断点调整模拟 `adjusted` | `breakpoint_misplaced`；仅计调整相对已覆盖量增加的部分，不代表所有未来检查点策略 |
| 3a：存在覆盖缺口 | 先到 `gap_start`，再到 `stable_tokens` | 前一段依次检查 `adjusted1h`、无限回看 `lookback`、忽略完成门槛 `pending` 是否到达 `gap_start`，取首个满足者归 `ttl_expired` / `lookback_exceeded` / `concurrent`，否则归 `stable_unexplained`；后一段归 `breakpoint_stalled` |
| 3b：无缺口且前后两轮都有断点 | 独立滚动推进模拟 `advanced` | `lookback_exceeded`；正常推进端点间的大跨度跳跃不算覆盖停滞 |
| 3c：前后任一轮没有断点 | `max(fallback, advanced)` | `no_breakpoint`；需要此前写入与当前读取两侧断点，不能只补当前读端点就假设历史已写入 |
| 4 | `max(adjusted1h, advanced1h)` | `ttl_expired`；仅分配超过已覆盖量的部分 |
| 5 | 当前模拟已有、已就绪且仍存活的锚点，取消回看窗口限制 | `lookback_exceeded`；不虚构每块写入 |
| 6 | 当前模拟保留回看/过期约束，但忽略 `availableAt` 门槛 | `concurrent`；只表示潜在并发未就绪 |

4. **稳定旧历史与覆盖缺口的定义（§7.12 当前口径）**：旧历史取两轮都存在的块范围；参数变化时 `stable_tokens=0`，否则取首个分叉之前的旧前缀，未分叉时取全部旧历史，均按本轮字节占比换算 token。`gap_start` 是前后两轮所有已观察断点中最远端点的累计 token。仅当前后两轮都有断点且 `stable_tokens > gap_start` 时判覆盖缺口：稳定旧历史超出了两轮端点覆盖。它表示结构覆盖不足，不保证假设的写入能存活到本轮；正常推进端点已覆盖旧历史时，即使旧锚点过期或超出回看，也不判 stalled。
5. **稳定段剩余量**：上述候选项后，`stable_tokens > covered` 的差额归 `stable_unexplained`（稳定前缀原因未明）。哈希相同不证明曾写缓存、仍未过期或本轮一定可读，不能直接归 `upstream_miss` / `channel_switch`。
6. **真正变化的旧历史**：参数变化归 `param_changed`，否则旧历史内的内容分叉归 `prefix_changed`；仅分配 `max(0, old_tokens-covered)`，不重算已分配量。
7. **`new_content`**：最后剩余量归此项；旧稳定前缀已在前面独立处理，不再因模拟上限耗尽而混入新内容。

同一轮同一原因可能在瀑布中出现多段，返回前合并 token 且 `turns=1`；汇总中同一原因每轮只计一次。不同原因的影响轮次仍可重叠，不能相加当总轮数。

数量分解**没有 5% 截断**：所有正增量都计入。既有逐轮 `reason` 主标签仍采用原优先级和部分原因超过本轮 5% 的显示阈值，它是摘要标签，不是 `losses` 的数量算法；标签为 `hit` 也可能存在小额未缓存分解，不能按标签计数来推导 token 损失。

### 7.6.5 估算边界与交付状态

- 总输入与实际读取来自记录的 usage；前缀各段 token 按本轮块字节占比分摊，不是逐块真实 tokenizer 结果。中文、代码、图片、工具结构等比例可能不同；结果用于定位和比较，不承诺精确金额或节省比例，也不改计费结算。
- 模拟在同一 lane 回放，缓存键为参数指纹起始的滚动前缀哈希；渠道/key 是诊断相关信息，不是真实缓存域标识。`channel_switch` 不证明实际缓存域丢失，`upstream_miss` 也不能直接证明供应商故障；上游内部路由、驱逐、未观察到的预热及模型近似都可能产生差额。此前“责任方”“一定能挽回”等表述不能作为本阶段的因果或收益承诺。
- 缓存可读时间保守地取 `end_ts`（响应完成）。现有骨架缺少上游首响应时间，而缓存可能在流式响应结束前已经可读；忽略就绪门槛的并发探针只表明潜在重叠，不能证明必须等完整响应，更不能据此默认开启冷并发等待。
- 当前回看窗口是 20 个原始块位置的近似；并行工具调用的供应商分组可能不同，`lookback_exceeded` 需要结合具体上游解释。无限回看探针只检查实际模拟中已写入且未过期的锚点，不虚构任意块缓存。
- 仅记录块级断点的旧骨架不一定完整反映顶层自动缓存；`bp_idx` 缺失时无法排除自动缓存存在。因此 `no_breakpoint` 是有条件的反事实估算，不是“客户端肯定没有启用缓存”的结论。顶层缓存信息未记录时不能事后恢复。
- 本次阶段 1 已交付查询时统计、后端分类汇总、单会话与筛选汇总展示、成功写入锚点保留、基础前缀检查点及对应回归测试；保留已有章节及历史验证记录。其他运行时优化仍未开启。
- 已完成验证：`gofmt`；`go test ./model ./controller -run 'Test(Analyze.*CacheSkeletons|ClaudeCacheLoss)' -count=1` 通过；前端缓存损失组件 focused tests 22 个通过、涉及文件 lint 无错误、格式化和 `git diff --check` 通过、生产构建通过、en/zh scoped locale sync 通过。全仓 TypeScript 检查仍有与本次改动无关的既有错误，未将其扩大修复范围。
- 尚未完成真实 ClickHouse 会话数据核对和真实浏览器/后端联调，因此当前结果是可复现的估算统计能力，不代表真实流量中的损失分布，也不把全部未缓存 token 视为可节省量。
- 成功锚点保留另已通过 relay 断点回归测试和 `go test ./relay ./model -count=1`；真实上游缓存写入位置无法从响应逐断点确认。运行时状态的持久化范围见 §7.10：本节描述时仅在当前进程内有效，§7.10 之后 Redis 启用时跨实例共享。

## 7.7 客户级 Claude 缓存优化（2026-11-03）

本节替代 §7.6 中有关共享开关的描述；损失量估算口径保持不变。客户列表操作区提供编辑与统计图标，编辑抽屉新增 Claude 缓存命中率优化分区。

- 五项独立配置：强制 1h TTL、修复易变尾部断点、保留上一轮成功写入位置、稳定基础前缀断点、无缓存标记客户端兜底（§7.8）。每项包含 `enabled` 和 `model_prefixes`。
- 开关开启且前缀列表为空，匹配 Claude 请求路径下所有适用模型；列表非空时按模型名的字面前缀匹配，不需 `*`，忽略大小写。关闭不清除前缀选择。匹配采用渠道及重试参数覆盖后的出站模型名。
- 未配置的旧客户继承原有行为；点击自定义后，客户策略完整覆盖旧规则，显式关闭不会被旧全局开关重新开启。旧 API 不传该对象时更新客户不会清除已保存策略。旧四项配置缺少兜底字段时，该项默认关闭。
- 客户身份仅使用可信 trace-tag 解码结果。配置存主库独立 TEXT 列 `claude_cache_optimization_json`，API 使用对象 `claude_cache_optimization`；客户配置缓存通过既有多实例同步机制传播。
- 保留上一轮断点的运行时状态与 ClickHouse 采集解耦，显式开启时无需诊断数据库。状态存储见 §7.10：Redis 启用时跨实例保留，未启用时只在当前进程内保留，重启和切换网关节点可能失去锚点。透传请求体路径不执行这些改写。供应商仍须支持相应缓存能力，开启开关不会为模型增加供应商未提供的能力。
- 客户统计复用按 `customer_id` 过滤的日志分析及会话缓存损失量分析，支持日期选择。缓存骨架诊断依赖 ClickHouse；范围分析先选中会话再回放保留的完整生命周期，可能覆盖筛选时间外请求，与严格时间窗口内的常规日志统计口径不同。
- 运行时优化在渠道与重试参数覆盖后执行，记录实际最终断点；`cache_creation` 只能证明该请求有写入，无法逐断点确认供应商保存了哪些位置。
- 验证记录：真实 SQLite 3.50.4、MySQL 8.2.0、PostgreSQL 18.4（aarch64）均通过客户表 fresh/旧表升级及第二次 AutoMigrate 无 DDL 验证，原字段与唯一约束保留。命令为设置隔离库 `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN` 后执行 `/usr/local/go/bin/go test ./model -run '^TestCustomerClaudeCacheOptimization' -count=1 -v`。测试容器及临时数据已清理。
- 前端验证：客户 focused tests 8/8（单 worker，60s 超时）、客户模块 lint 零错误/警告、涉及文件格式检查及 en/zh 文案检查通过；`bun run build` 通过。`bun x --no-install tsgo -b` 仍报客户模块之外的 9 个既有类型错误，未扩大修改范围。未进行真实浏览器联调或真实上游命中率收益验证。
- Relay 验证：`gofmt` 和 `/usr/local/go/bin/go test ./relay -count=1` 通过，覆盖独立策略与继承、显式关闭优先级、无诊断数据库的锚点读写、完整前缀/角色/消息边界/参数校验、逐断点过期及旧请求防覆盖、最新锚点优先和自动缓存占用槽位。模型层及控制器层全量测试亦通过。锚点状态按客户、令牌、会话、渠道/Key、模型、lane 和参数指纹隔离；当时的存储实现为进程内 map，容量上限 4096 份快照，活动时清理过期项。该存储已于 §7.10 迁移到 Redis/进程内混合缓存，本条验证记录的"进程内 map、4096 上限"描述对应迁移前的实现，§7.10 为迁移后的现状。

## 7.8 无 cache_control 客户端兜底（2026-11-03）

- 客户配置新增 `fallback_cache_breakpoint`，具有独立 `enabled` 与 `model_prefixes`，匹配规则与其他优化相同。默认关闭，旧客户不自动启用。
- 在渠道/重试参数覆盖之后、易变尾部修复/基础前缀/历史锚点之前判断。顶层、tools、system 或消息内容已有 `cache_control` 就不补点；即使字段为 null 或非法值，也保留客户端配置供原校验链路处理。不把工具输入/schema 内的同名业务字段当成缓存标记。
- 在最后一个合法内容块添加 `{"type":"ephemeral","ttl":"5m"}`；跳过 thinking、redacted_thinking、空文本和非 `never` 的 clear_at 临时消息。消息内没有候选时向前查找 system/tools。字符串内容等价转换为单个 text 块，不改变文字。
- 仅兜底本身最多新增一个断点；之后可按独立开关添加基础前缀、修复易变尾部、保留旧锚点。强制 1h 规则在后执行，能将新增兜底断点升级为 1h。重复执行不重复加点。
- 透传请求体路径不执行兜底；不依赖 ClickHouse，不修改计费或 usage。供应商最小缓存 token 阈值与缓存能力仍然适用，新增断点不保证短请求一定产生缓存读写。
- 验证：`gofmt`、`go test ./relay -count=1` 通过，覆盖已有标记跳过、合法目标选择、字符串内容保留、幂等和强制 1h 联动。客户策略前端 5 个用例、lint、格式检查、en/zh scoped sync 及生产构建通过；真实 SQLite 3.50.4、MySQL 8.2.0、PostgreSQL 18.4 的客户策略测试通过，包含旧四项 JSON 默认关闭第五项、配置保存/深拷贝以及 fresh/升级/重复迁移检查。本次仅扩展配置 JSON，未新增表列。真实浏览器联调和上游命中率收益尚未验证。
- 本轮复跑 `bun x --no-install tsgo -b` 仍有 9 个范围外类型错误，位于渠道测试夹具、系统设置的代理配置及使用日志测试；客户策略相关文件没有类型诊断。全仓类型检查尚未通过。

## 7.9 同会话缓存优化覆盖率对比（2026-11-03）

- 比较的是相同会话内容下，原始策略与实际出站策略的预计可读取前缀字节占比。基线位于渠道/重试参数覆盖和必要的 `clear_at` 协议清理之后、五项可选缓存优化之前。对比五项策略的整体净效果，不把单项重叠收益相加。
- 单独记录规范化结构，不改变既有 `blk_hash`。字符串内容等价表示为单 text 块，排除协议块上的 `cache_control` 字节；指纹包含角色、消息边界与内容，工具 schema 内同名业务字段保留。仅存指纹、长度、断点与 TTL，不存请求原文。
- 原始与优化策略使用相互独立的历史；按令牌、会话、模型、渠道/Key 隔离，前缀链还包含参数指纹。写入在响应完成后可见，按各自断点和 TTL 回放，保留 20 块回查窗口，模型最小 token 阈值按字节比例估算。
- 每轮返回原始覆盖字节、优化覆盖字节、带符号差值与输入字节。汇总按总覆盖字节除以总输入字节加权，不平均逐请求百分比；负收益保留。首次可比较请求计入分母但从冷状态开始。
- 仅成功且有 usage、双侧结构完整且规范化内容一致的 Claude 请求纳入比较。旧数据、请求体透传和不完整记录不伪造基线，显示升级后记录才可比较；遇到缺失结构后重置对应回放历史。历史保留/扫描截断可能遗漏早期写入，沿用页面部分样本提示。
- 同时保留真实上游 token 命中率，字节覆盖率是结构与 TTL 回放估算，不等于供应商确认的节省 token。未建模的上游驱逐、内部路由和跨会话缓存共享会影响实际结果。
- 页面入口：单会话诊断、展开后的 lane、会话筛选范围汇总、客户统计弹窗共用紧凑摘要，展示基线→优化后、带符号百分点/字节变化及可比较轮次；缺少样本显示不可用。复用现有字节/数字格式化及布局，本次只新增业务摘要，不新增通用交互组件。
- 后端验证（Go 1.26.10）：`gofmt`、`/usr/local/go/bin/go test ./model ./relay ./controller -count=1` 最终通过。回归覆盖独立历史、TTL 正负收益、并发写入完成时序、20 块回查边界、参数/渠道/令牌/Key 隔离、模型写入门槛和旧记录后恢复采集。最小 token 门槛仅在模拟写入时检查，不因后续尾部字节密度改变而否定已写入前缀。
- 数据库验证：真实 ClickHouse 24.8.14.39 验证 fresh、旧表补列、重复迁移、旧记录默认空数组及 UInt64/UInt32 边界数组往返；SQLite 3.50.4、MySQL 8.2.0、PostgreSQL 18.4 验证主库迁移保护。命令为设置隔离库的 `TEST_CLICKHOUSE_DSN`、`TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN` 后运行 `/usr/local/go/bin/go test ./model -run 'TestCacheSkeleton(ClickHouseRoundTrip|SchemaParses)$|TestCustomerClaudeCacheOptimizationMigration$' -count=1 -v`，通过；测试容器及临时卷已清理。
- 前端验证：4 个相关 Vitest 文件共 34 项通过，定向 oxlint 8 文件零错误/警告、oxfmt 10 文件通过、`bun run build`、`bun run i18n:sync --locales en,zh` 及 `git diff --check` 通过。`bun x --no-install tsgo -b --pretty false` 仍有 9 处既有范围外类型错误，本次覆盖率代码无新增类型报错。未做真实浏览器联调或真实上游收益核对。

## 7.10 审查修复：全局配置解耦、兜底断点感知易变标记、透传标记、渠道亲和自检、锚点状态迁移 Redis（2026-11-04）

针对一次独立代码审查提出的问题，做了以下改动；损失量估算口径、骨架表结构不变。

- **全局五项规则解耦**：此前 `StableBaseBreakpoint` 在全局层面复用 `ShouldFixCacheBreakpoints` 同一个开关，`FallbackCacheBreakpoint` 全局层面无入口，`RetainPreviousBreakpoint` 隐式绑定在 `CacheSkeletonEnabled()`（即诊断库是否为 ClickHouse）上。现在三者各自拥有独立的 `GroupModelRuleSetting`（`stable_base_breakpoint_setting`、`fallback_cache_breakpoint_setting`、`retain_previous_breakpoint_setting`），系统设置页面新增对应编辑入口；客户级覆盖（§7.7）行为不变，仍是五项独立开关。关闭缓存诊断日志不再连带关闭"保留上一轮断点"这一改请求体的行为；§7.6.1 第 1 项、§7.7 中"依赖诊断数据库""与 ClickHouse 采集解耦"等描述维持有效，新增的独立开关只改变全局默认值的来源，不改变这条解耦关系本身。
- **易变标记识别范围扩大**：`addClaudeFallbackCacheBreakpoint`（兜底断点）此前未检查易变尾部标记，可能把兜底断点打在每轮都变的块上（每轮写、从不读）。现在复用与 `fixClaudeCacheBreakpoints`、`addClaudeStableBaseBreakpoint` 相同的 `claudeTextHasVolatilePattern` 判断。易变标记列表本身从代码常量改为可配置项 `claude_volatile_tail_pattern_setting`（系统设置页面可编辑），为空时退回内置默认值 `["<total_tokens>"]`；§6.1、§6.2 中"特征串默认只放 `<total_tokens>`""P1 上线后根据骨架跨轮比对结果补充"的计划现在有了配置通道，不必再改代码发版。
- **透传模式诊断标记**：透传请求体路径（§5.2.1、§7.7）此前跳过全部五项优化且无任何痕迹，客户配置了策略也无从得知被跳过。现在透传路径下，若该客户/分组模型组合实际配置了任意一项优化，骨架记录的 `fixed` 字段写入新值 `pass_through`；骨架弹窗对应显示"已跳过：透传模式"。未配置任何优化的客户不受影响，不产生额外标记噪音。
- **渠道亲和覆盖率自检**：§7.6.1 第 4 项提到的"真正的上游缓存域亲和性"目前仍由既有的 `channel_affinity_setting` 承担（内置规则 `claude cli trace`），与本文档描述的五项缓存优化是两套独立配置。新增 `operation_setting.CheckClaudeCacheAffinityCoverage`：当任一全局缓存优化规则非空、但 `channel_affinity_setting` 未启用或没有规则的 `ModelRegex` 能匹配 Claude 模型名时，输出一条 `SysLog` 警告。在网关启动（`InitOptionMap`）及运维通过管理界面修改渠道亲和或五项缓存优化任一项配置时（`handleConfigUpdate`）都会重新检查。这是启发式自检，针对一个代表性 Claude 模型名探测，不覆盖客户级覆盖（本包看不到 `model.CustomerClaudeCacheOptimization`），也不是强制校验，只是运维提示。
- **优化步骤失败降级**：`addClaudeFallbackCacheBreakpoint`、`fixClaudeCacheBreakpoints`、`addClaudeStableBaseBreakpoint`、`retainClaudeCacheBreakpoints` 任一步骤返回 error，此前会让整个请求失败且不重试（`ErrOptionWithSkipRetry`）。这些步骤本质是锦上添花的优化，跳过它们请求仍然合法。现在改为记录日志（`logClaudeCacheStepError`）并跳过该步骤，继续后续流程，不影响请求成功与否。
- **`clear_at` 可缓存性判断统一**：`addClaudeFallbackCacheBreakpoint` 与 `retainClaudeCacheBreakpoints` 此前对"`clear_at:"never"` 的消息是否可安全打断点"给出了不同答案（前者认为可以，后者认为不行），现已抽成共享函数 `claudeClearAtCacheable`，统一按"不存在或为 `never`"判定为可缓存。
- **锚点状态存储迁移 Redis（对应 §7.6.1 第 1 项、§7.7 中"运行时锚点仍为进程内短期状态"的遗留问题）**：`claudeCacheBreakpointStates`（进程内 `sync.Mutex` + map，容量上限 4096，按最早过期淘汰）替换为 `pkg/cachex.HybridCache[claudeCacheBreakpointState]`，与 `service/channel_affinity.go` 的渠道亲和缓存复用同一套 Redis/内存混合缓存抽象（`Namespace`、`RedisCodec`、`RedisEnabled` 均为同一模式）：
  - **`common.RedisEnabled && common.RDB != nil` 时**：走 Redis，`JSONCodec` 序列化整条状态（含 `[]time.Time`、`[]string` 等切片字段），用 Redis 原生 TTL（`SetWithTTL`，TTL 取 `time.Until(state.Expires)`）管理过期，不再有固定容量淘汰——多实例部署下，只要负载均衡没把同一会话打到不同实例，锚点现在能跨实例、跨进程重启读到。
  - **未启用 Redis 时**：退回进程内 `hot.LRU` 缓存（同样容量 4096，带 Janitor 后台清理），行为与迁移前等价，是该功能原有的"仅单实例有效"模式的延续，而非新增限制。
  - 命名空间 `new-api:claude_cache_breakpoint_state:v1`；读写接口（`getClaudeCacheBreakpointState` / `rememberClaudeCacheBreakpointState`）签名不变，调用方（`relay/claude_handler.go`）无需改动。
  - 保留原有"较旧的写入不得覆盖较新快照"的检查（按 `row.Ts` 比较），但在 Redis 路径下这是非原子的 read-modify-write：多实例并发写同一 key 时存在竞态窗口，后果仅为"多保留一会旧快照"，不会返回比实际写入更新的快照，也不会造成数据错乱。
  - §7.6.1 第 1 项、§7.6.5、§7.7 中原先"状态为进程内短期状态""最多 4096 份快照"等描述对应迁移前实现；本节为现状，之前章节的该类描述已在正文中加注指回本节。
  - 验证：`go build ./...`、`go vet ./...`、`go test ./relay ./setting/... ./model/... ./service/... -count=1`、`go test ./relay -race` 均通过。新增 `relay/claude_cache_breakpoint_state_redis_test.go`，用 `miniredis` 独立验证 Redis 路径的 JSON 序列化往返（含切片字段）及 TTL 过期行为（`miniredis.FastForward` 确定性测试，不依赖真实 sleep）；未在真实 Redis 集群或多实例部署下验证跨实例命中效果。

验证范围说明：本节改动均不涉及 ClickHouse 骨架表结构、计费逻辑或客户配置 schema；仅涉及 Go 后端行为、新增/调整系统设置项及前端对应编辑入口、诊断弹窗文案。前端改动（新设置项表单、骨架弹窗 `pass_through` 标签、诊断页面免责声明文案）已过 `tsc --noEmit` 与 `oxlint`（改动文件范围内），未做真实浏览器联调。

## 7.11 历史阶段：旧稳定前缀混入 new_content 的首次修复（2026-11-05，已由 §7.12 纠正）

> 本节保留旧阶段背景与验证记录，不代表当前归因规则。该阶段把稳定前缀剩余量直接归上游的做法证据不足，已由 §7.12 的 `breakpoint_stalled` / `stable_unexplained` 分解替代；下列“修复”“正确拆分”均仅描述当时实现和断言，不能用来证明上游责任。现行算法以 §7.6.4 为准。

- **触发原因**：客户反馈会话诊断里绝大部分未命中都被归到 `new_content`，但实际观察到命中率波动和上游渠道内部账号池是否稳定高度相关（账号池死号被替换/补号时命中率从 80%+ 跌到 60%-70%），怀疑诊断结论低估了上游因素。
- **根因**（§7.6.4 第 4 步原实现）：`skeletonTurnLosses`（`model/cache_skeleton_loss.go`）在计算旧历史范围内"哪些块哈希没变"（`stable_tokens`）时，只在检测到分叉（即存在部分内容变了）时才使用这个值，用来 clamp 结构变化那一步的下限；如果旧历史范围内的块哈希**完全没变**（`DivergeIdx == -1`），这段计算整个被跳过，`stable_tokens` 从未被单独拿去检查"是否已经被前面的候选项覆盖"。当模拟器自身的命中预测（`current`/`adjusted`/`adjusted1h`/`lookback_exceeded` 等候选项）因为回看窗口（固定 20 个原始块）或断点 TTL 建模偏差而判断不出预期命中时，这段哈希证实没变的差额会连同真正的新增内容一起，全部滑进瀑布最后一步的兜底项 `new_content`。这个机制和渠道、key 是否变化完全无关：即使网关配置的渠道/key 全程不变（只是上游在那个 key 背后做了账号轮换），只要模拟器自己先判不出命中，诊断结果也会显示成"新内容"而非"上游未命中"，掩盖了真实的上游因素。
- **修复**：`stable_tokens` 改为无条件计算（不再依赖是否检测到分叉），并在瀑布走完全部模拟器候选项之后、落入 `new_content` 之前，单独加一步 clamp——若 `covered` 还没推进到 `stable_tokens`，差额计入第 1 步已经判定好的标签（`channel_switch` 或 `upstream_miss`，取决于本轮渠道/key 是否相对上一轮变化），而不是放任它漏到 `new_content`。真正发生分叉的段落（`param_changed`/`prefix_changed`）逻辑不变，仍建立在这一步 clamp 之后的 `covered` 上，token 账本不重不漏。详见更新后的 §7.6.4 第 4 步。
- **影响范围**：只影响损失归因展示（会话诊断的逐轮分解、汇总报表），不改变 ClickHouse 骨架表结构、不改变实际发往上游的请求、不改变计费。此前把这类差额算作 `new_content` 的历史诊断记录在重新查询时会按新逻辑重算，不需要重新采集。
- **验证**：`go build ./...`、`gofmt`、`go vet ./model/...` 通过；`go test ./model/... -count=1`、`go test ./controller/... -count=1`、`go test ./relay/ -count=1` 通过。修正了一个因本次改动而断言发生变化的既有用例（`TestClaudeCacheLossSummary/prefix_change_excludes_stable_prefix_and_new_blocks`，该用例本身在验证旧的错误行为，现在断言三段式拆分：`upstream_miss`/`prefix_changed`/`new_content`）；新增 `TestClaudeCacheLossStablePrefixSurvivesExhaustedSimulatorCeilings`，构造一个前缀哈希完全没变、但间隔 2 小时（超过所有模拟器候选项能解释的窗口，包括 1h TTL 档）的两轮请求，验证过该测试在修复前会把全部 30000 token 错记为 `new_content`，修复后正确拆分为 `upstream_miss: 18000` / `new_content: 12000`。`relay/channel/openai` 下的 `TestOaiResponsesStreamHandlerCompletionLifecycle` 若干子用例失败，是预先存在、与本次改动无关的既有 flaky 测试（在未修改代码的原始分支上同样失败，已单独确认）。
- **未验证**：这次修复是基于"骨架哈希比对能证明内容没变"这一前提做的归因口径调整，没有新增任何能直接识别"上游账号池是否波动"的信号；真实账号池波动场景下诊断结果是否与客户的运维观察（死号/补号时间点）对齐，仍需要在真实流量上验证，不在本次改动范围内。

## 7.12 本次修复：断点覆盖推进与稳定前缀归因（2026-11-05）

- **归因纠正**：内容未变只说明旧前缀稳定，不能推出“上游必须命中”。先解释实际已观察策略的 `current - actual_read`，再处理尾部位置、覆盖缺口及 TTL/回看/并发，最后将仍无法解释的稳定段归 `stable_unexplained`。固定早期断点留下的稳定旧历史缺口归 `breakpoint_stalled`，表示结构覆盖不足，不是保证可回收 token，也不是上游账号池波动的直接证据。详细顺序与边界见 §7.6.4。
- **独立滚动策略**：新增“推进”与“推进 1h”两套独立写入历史，不借用当前策略历史、不用未来轮次选点，也不将其当作无限回看探针。优先选择本轮最后一个合法可缓存块、前次策略中前缀仍一致的最远端点，再补原断点，去重后最多 4 个；末尾 system 消息区段保守排除，空块、thinking / redacted_thinking 不作端点。读取仍限含端点自身的 20 个原始 block（`bp-19 … bp`），写入受模型最小缓存长度约束。
- **TTL 与时序**：“推进”新尾端点继承本轮最后一个原断点的 TTL，无原断点时默认 5m；保留端点携带原 TTL，排序后约束早期长 TTL、后期短 TTL，必要时缩短后点而不暗中升级 5m。“推进 1h”将选中端点统一为 1h。两套历史都只在写入请求完成（`end_ts`）后生效，按完成顺序落入历史；在途请求不能提前创建缓存或延长已过期项。
- **API 与展示**：Claude 汇总新增 `advanced`、`advanced_1h`，逐轮新增 `expected_advanced`、`expected_advanced_1h`。逐轮为原始模拟读取估计；汇总逐轮以实际读取为下限后加总。字段语义仅用于 Claude，OpenAI 保持原诊断且这四项为 0，混合供应商汇总不能直接当 Claude 推进率。尾部修正 `adjusted` / `adjusted1h` 与滚动推进分别展示，独立策略结果不保证递增，也不能把相邻差额或各项收益相加。数量瀑布中相同原因先合并再计一次影响轮次。
- **影响范围**：仅查询时回放、归因及诊断展示；不改变在线请求、数据库 schema/SQL/采集格式或计费结算。已有骨架可重查重算，缺失骨架不能补造；截断、覆盖未知及 token 按字节比例估算的边界继续适用。
- **已跑验证（按本次修复提供的结果记录）**：`go test ./model -count=1` 通过；前端定向测试 27 项通过；lint、format、build 通过。typecheck 未通过：3 个未修改文件仍有 7 处既有错误，不能称为“全部检查通过”。本次文档更新未重新运行这些代码检查。
- **尚未验证**：真实 case 未联调，未核对真实上游账号池事件、缓存命中与推进收益；结构缺口和模拟增量不能替代真实收益验证。

## 8. 待定项

- [x] 现在的 `LOG_DB` 就是这台单机 ClickHouse（2026-10-31 确认）。
- [x] 日请求量和磁盘剩余空间已实测（见 §5.4），磁盘足够。
- [x] 按块存，保留 14 天，放在 `default` 盘（2026-10-31 按实测数据定）。试点后用 `bytes_per_row` 复核。
- [x] 日志库不是 ClickHouse 时，功能直接关闭（2026-10-31 确认）。
- [x] 归因报表列出"渠道切换"等网关侧原因。报表供内部使用，需要时脱敏后给客户（2026-10-31 确认）。
- [x] 断点调整的判定：带 `clear_at` 或命中特征串时，允许新增或挪动断点；只是"末尾 `role: "system"`"时，只允许新增。特征串默认只放 `<total_tokens>`，P1 上线后根据骨架跨轮比对出的易变块补充（2026-10-31）。
- [x] 单机 ClickHouse 多盘：不需要。数据盘全是 NVMe，`default` 盘剩余 810GiB 足够（见 §5.4、§5.7）。

## 附录 A：运维命令（单机 ClickHouse，yq01-cdn-iaas-monitor01）

以下命令都在**宿主机 shell** 里执行，提示符是 `[root@... ~]#`。不要在 ClickHouse 客户端（提示符 `:)`）里粘贴。密码直接读容器内的环境变量，不用手打。

**A.1 交互连接**

```bash
docker exec -it newapicl_newapi-31525-clickhouse-1 bash -c \
  'clickhouse-client -u "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" -d "$CLICKHOUSE_DB"'
```

进入后可以直接输入 SQL，用 `exit` 退出。

**A.2 查看数据目录挂载在宿主机哪块盘**

```bash
docker inspect -f '{{range .Mounts}}{{.Source}} -> {{.Destination}}{{"\n"}}{{end}}' newapicl_newapi-31525-clickhouse-1
df -h /home/work/newapi-clusters/cluster/newapi-31525/volumes/clickhouse
```

**A.3 容量评估（一次跑完，即 §5.4 实测基线所用的命令）**

```bash
docker exec -i newapicl_newapi-31525-clickhouse-1 bash -c \
  'clickhouse-client -u "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" -d "$CLICKHOUSE_DB" --multiquery --format PrettyCompactMonoBlock' <<'SQL'
-- 版本 / 时区
SELECT version() AS ver, timezone() AS tz, currentDatabase() AS db;
-- ClickHouse 看到的磁盘
SELECT name, path, formatReadableSize(free_space) AS free, formatReadableSize(total_space) AS total FROM system.disks;
-- 各表占用与压缩比
SELECT table, sum(rows) AS rows, count() AS parts,
  formatReadableSize(sum(data_compressed_bytes)) AS compressed,
  formatReadableSize(sum(data_uncompressed_bytes)) AS raw,
  round(sum(data_uncompressed_bytes) / greatest(sum(data_compressed_bytes), 1), 1) AS ratio
FROM system.parts WHERE active AND database = currentDatabase()
GROUP BY table ORDER BY sum(data_compressed_bytes) DESC;
-- 近 7 天每天 Claude 消费请求量(北京时间切天)
SELECT toDate(toDateTime(created_at, 'Asia/Shanghai')) AS day, count() AS claude_reqs,
  countIf(JSONExtractInt(other, 'cache_tokens') > 0 OR JSONExtractInt(other, 'cache_creation_tokens') > 0) AS with_cache
FROM logs
WHERE type = 2 AND JSONExtractBool(other, 'claude') AND created_at >= toUnixTimestamp(now() - INTERVAL 7 DAY)
GROUP BY day ORDER BY day;
-- 近 7 天峰值小时请求量
SELECT max(c) AS peak_hour_reqs, round(avg(c)) AS avg_hour_reqs FROM (
  SELECT toStartOfHour(toDateTime(created_at)) AS h, count() AS c FROM logs
  WHERE type = 2 AND JSONExtractBool(other, 'claude') AND created_at >= toUnixTimestamp(now() - INTERVAL 7 DAY)
  GROUP BY h);
-- 近 1 天 Claude 单请求总输入 token 分布(prompt + 缓存读 + 缓存写)
SELECT quantiles(0.5, 0.9, 0.99)(prompt_tokens + JSONExtractInt(other, 'cache_tokens') + JSONExtractInt(other, 'cache_creation_tokens')) AS p50_p90_p99_input,
  round(avg(prompt_tokens + JSONExtractInt(other, 'cache_tokens') + JSONExtractInt(other, 'cache_creation_tokens'))) AS avg_input
FROM logs
WHERE type = 2 AND JSONExtractBool(other, 'claude') AND created_at >= toUnixTimestamp(now() - INTERVAL 1 DAY);
-- logs 表每行实际占用(参照物)
SELECT round(sum(data_compressed_bytes) / greatest(sum(rows), 1)) AS bytes_per_row
FROM system.parts WHERE active AND database = currentDatabase() AND table = 'logs';
SQL
```

- 字段约定：`type = 2` 表示消费日志；`created_at` 是 Unix 秒；`other.claude`、`cache_tokens`、`cache_creation_tokens` 来自 `service/log_info_generate.go`。Claude 日志的 `prompt_tokens` 不含缓存部分。
- 扫描 7 天的 `other` JSON 可能要几十秒。太慢时把 `7 DAY` 改成 `3 DAY`。

**A.4 骨架表试点监控（P1 上线后）**

```bash
docker exec -i newapicl_newapi-31525-clickhouse-1 bash -c \
  'clickhouse-client -u "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" -d "$CLICKHOUSE_DB" --multiquery --format PrettyCompactMonoBlock' <<'SQL'
-- 每行实际大小 + part 数(part 多而 bytes_per_row 不降 = 合并没跟上,按最坏情况规划)
SELECT count() AS parts, sum(rows) AS rows,
  formatReadableSize(sum(data_compressed_bytes)) AS c,
  round(sum(data_compressed_bytes) / greatest(sum(rows), 1)) AS bytes_per_row
FROM system.parts WHERE active AND database = currentDatabase() AND table = 'cache_skeletons';
-- 按天分区占用 / 所在磁盘
SELECT partition, disk_name, sum(rows) AS rows, formatReadableSize(sum(data_compressed_bytes)) AS c
FROM system.parts WHERE active AND database = currentDatabase() AND table = 'cache_skeletons'
GROUP BY partition, disk_name ORDER BY partition;
-- 各列压缩情况
SELECT name, formatReadableSize(data_compressed_bytes) AS c, formatReadableSize(data_uncompressed_bytes) AS u
FROM system.columns WHERE database = currentDatabase() AND table = 'cache_skeletons';
SQL
```

## 附录 B：骨架示例（`<total_tokens>` 断点错位）

块序号按 tools → system → messages 编号。哈希示意为 4 位十六进制，实际是 64 位。`blk_kind` 的写法：`tool` / `sys` 表示顶层 tools / system，`u.` / `a.` 表示 user / assistant 消息，`ms.` 表示 messages 里的 `role: "system"` 消息。

**第 1 轮（客户端原样，断点在 #18、#25）：**

| # | msg | blk_kind | blk_hash | blk_len | 断点 |
|---|---|---|---|---|---|
| 0–14 | – | tool ×15 | 3a0e…91c2 | … | |
| 15–18 | – | sys ×4 | 77b1…e4d0 | 174…15661 | #18 5m |
| 19, 20 | m0 | u.text, u.text | 5c3f, 0d9a | 12358, 41 | |
| 21 | m1 | ms.text | b812 | 21210 | |
| 22, 23 | m2 | a.thinking, a.tool_use | 6e04, f1a7 | 2180, 397 | |
| 24 | m3 | u.tool_result | 9c55 | 8003 | |
| 25 | m4 | ms.text | **7c21** | 52 | **#25 5m** |

usage：`cache_read=25100`，`cache_creation=48900`，`input=3`。

**第 2 轮（模型又调用一次工具，提醒数值变化并移到末尾）：**

| # | msg | blk_kind | blk_hash | blk_len | 断点 |
|---|---|---|---|---|---|
| 0–24 | m0–m3 | 同第 1 轮 | 同第 1 轮 | 同第 1 轮 | #18 5m |
| 25, 26 | m4 | a.thinking, a.tool_use | 2b90, 44d3 | 1904, 211 | |
| 27 | m5 | u.tool_result | e07a | 3120 | |
| 28 | m6 | ms.text | **a3f8** | 52 | **#28 5m** |

usage：`cache_read=25100`，`cache_creation=51200`。

**查询时归因（第 2 轮）：**

- 和第 1 轮逐块比对，#0–#24 相同，第一个分叉点是 #25：上一轮是 `ms.text`（并且是上一轮的末尾断点），这一轮是 `a.thinking`。
- 上一轮末尾断点 #25 的前缀在本轮已不存在，但 #24 之前的前缀都还在，因此归因为**断点错位**（客户端原因）。
- 应命中 ≈ 上一轮缓存总量 74000 − #25 的约 16 token ≈ 73984，实际 `cache_read=25100`（只命中 tools + system），**损失 ≈ 48884 token**。
- 判定依据只用到哈希、块类型、位置和断点，不涉及任何正文。

**开启断点调整后：** 第 1 轮网关在 #24（`u.tool_result`）新增断点（`fixed=added`），客户端原来的 #25 断点保留，于是第 1 轮会在 #24 处写入一份缓存。第 2 轮的 #28 断点往回查找（Anthropic 最多回溯 20 个块）时找到这份缓存，预期 `cache_read` ≈ #0–#24 的全部前缀约 73984 token，损失接近 0。第 2 轮网关同样在 #27 新增断点，供第 3 轮命中，以此类推。

### B.2 人读视图（日志详情 / 会话视图里的"查看骨架"）

单条请求按 tools → system → messages 分组缩进，每块一行：`#序号  类型  [名称]  哈希  大小  累计`。"累计"是从开头到这一块的字节数，用来看每个断点切下来的前缀有多大。`◆` 表示断点。`[易变]` 只在跨轮比对确认后才标出，即下一轮这个块消失或哈希变了。单条请求视图里无法确认，只把"断点打在最后一条 system 消息上"标为 `⚠`。

```
请求骨架  req 7f3a…  2026-10-30 14:03:22  node yq01-01  渠道 12 / key 3
model claude-sonnet-5-5   thinking adaptive   effort xhigh   参数指纹 4d1e
会话 3b9e… / lane 9b2c   断点 2 个(5m, 5m)   网关改写 -
usage  缓存读 25,100   缓存写 48,900   未缓存 3   命中率 34%

tools (15)                                      累计
  #0   tool         Bash         3a0e   1.0KB    1.0KB
  …
  #14  tool         WebFetch     91c2   0.6KB   11.8KB
system (4)
  #15  text                      77b1    174B   12.0KB
  …
  #18  text                      e4d0   15.3KB  29.1KB  ◆ 5m
messages (5)
  m0 user
    #19  text                    5c3f   12.1KB  41.2KB
    #20  text                    0d9a     41B   41.2KB
  m1 system
    #21  text                    b812   20.7KB  61.9KB
  m2 assistant
    #22  thinking                6e04    2.1KB  64.0KB
    #23  tool_use   Bash         f1a7    397B   64.4KB
  m3 user
    #24  tool_result             9c55    7.8KB  72.2KB
  m4 system
    #25  text                    7c21     52B   72.3KB  ◆ 5m  ⚠ 断点打在最后一条 system 消息上
```

和上一轮对比时，相同的前缀折叠成一行，只展开分叉点之后的部分：

```
第 2 轮 vs 第 1 轮   间隔 41s   同渠道同 key   参数指纹相同
  #0–#24   与上一轮相同(tools 15 / system 4 / messages m0–m3, 72.2KB)
  ✗ 分叉点 #25   上一轮: m4 system [易变] text 7c21 52B ◆   →   本轮: m4 assistant thinking 2b90
  m4 assistant
    #25  thinking                2b90    1.9KB  74.1KB
    #26  tool_use   Read         44d3    211B   74.3KB
  m5 user
    #27  tool_result             e07a    3.0KB  77.3KB
  m6 system
    #28  text                    a3f8     52B   77.4KB  ◆ 5m  ⚠
归因  断点错位(客户端)   应命中 ≈73,984   实际 25,100   损失 ≈48,884 token
建议  断点改打在 #24 tool_result(上一轮)/ #27 tool_result(本轮)
```

## 附录 C：讨论中的追问（FAQ）

**C.1 `<total_tokens>` 是一个单独的块吗？**
是 `messages` 数组里一条独立的 `role: "system"` 消息（mid-conversation system message），不是 user 消息里的 text 块。Claude Code 在每次 tool_result 后追加一条，形如 `<total_tokens>14992631 tokens left</total_tokens>`，数值每轮都变。客户端把消息区最后一个断点打在它上面。示例见附录 B，判定规则见 §6.1。

**C.2 `messages` 里 `role: "system"` 的消息都是易变块吗？**
不是。例如 `# Environment` 这条 system 消息会稳定地留在历史里。Anthropic 也建议旧的提醒原地保留。真正易变的是"最后一条、下一轮会被移走或改写"的那条。因此网关只对带 `clear_at`、或命中特征串的消息允许挪动断点；只是"末尾 system 消息"时，只新增断点。是否易变，以骨架跨轮比对的结果为准（§6.1）。

**C.3 每条请求骨架压缩后 0.8KB，是不是过于乐观？**
是。跨行压缩要靠 ClickHouse 后台合并，这一点没有保证，所以改为按"不跨行压缩"的最坏情况规划。结合实测数据，每天 75 万到 100 万条、保留 14 天，约需 24 到 81GB，单机放得下（§5.4）。

**C.4 哈希需要 64 位吗？CPU 消耗大不大？有没有更省的算法？**
哈希耗时只取决于输入字节数，和输出位宽无关。xxHash64 在实测里是最快的一档，并且仓库已有这个依赖。完整骨架提取中约 93% 的时间花在 gjson 遍历上，换哈希算法省不了什么。全集群峰值合计约 13ms CPU/秒。保留 64 位，抽样哈希会漏判，不采用（§5.2）。

**C.5 服务端是按字节缓存两个 `cache_control` 之间的内容（包括 JSON 的花括号）吗？**
不是。服务端先解析 JSON、渲染成模型输入，再对"从开头到断点"的整段 token 前缀做缓存，不单独缓存两个断点之间的片段。JSON 的结构符号和格式不进入缓存；字符串内容、块的类型和顺序、tool 定义、图片会进入。有多个断点就有多份前缀缓存，读取时从断点往前最多回溯约 20 个块。骨架按原始字节比较，比服务端更严格：不会漏判，但 JSON 格式变化时可能误报（§5.2）。

**C.6 改 `output_config.effort` 会让缓存失效，是因为值本身变了，还是因为它影响了输出？**
是值本身变了。缓存只针对输入前缀。effort 和 thinking 配置会被渲染进模型输入，值变了，渲染出的文本就不同，前缀也就变了。`max_tokens`、`temperature`、`stream` 同样影响输出，但不会被渲染进输入，所以不会让缓存失效。参数指纹因此包含 effort，不包含这几项（§5.2）。effort 具体渲染成什么、放在哪个位置，Anthropic 没有公开，以上是根据文档描述推断的。

**C.7 网关在第 1 次请求里改了 effort，用户第 2 次请求直连同一个 Bedrock 账号，缓存会失效吗？**
大概率会。第 1 次写入的缓存是按改后的值渲染的前缀，直连时带的是原值，前缀对不上。用户在两条路径之间来回切换时，每切换一次就有一次冷写入。

- 会导致两条路径的缓存互不相通的改写：改 effort 或 thinking、覆盖 system 提示词、thinking 转成 content、映射到不同模型。
- 不影响共享的改写：
  - 断点调整：只决定在哪里写缓存，不改内容。
  - 强制 1h TTL：只影响缓存保留多久，不改前缀内容。这一条是推断，Anthropic 没有明确说明。
  - 注入 `metadata.user_id`：metadata 不是模型输入。
- 前提：同账号、同 region、同模型才能共享缓存。用了 cross-region inference profile 时，不论走不走网关都可能不命中。
- 骨架看不到直连请求。混用两条路径的客户，在骨架里会表现为"条件都满足却未命中"（归为上游未命中），或者"凭空命中"。遇到这种情况，先问客户是否混用了路径。

**C.8 Claude Code 收到的响应里会带上网关改后的 effort 吗？它会跟着改吗？**
都不会。Messages 响应只有 `id`、`type`、`role`、`model`、`content`、`stop_reason`、`stop_sequence`、`usage`，不会回显请求参数。Claude Code 每一轮都按自己的设置（`/model`、配置文件、环境变量）重新拼参数，不会读上一轮的响应来调整。网关如果一直改，每一轮都要重新改。

唯一会被"带过去"的是内容：effort 不同，模型输出的 thinking 和回答也不同，这部分会作为历史在下一轮原样发回。不论下一轮走哪条路径，历史都是同一份，所以不影响 C.7 的结论。以上是根据 API 响应结构推断的，没有实际抓包；可以开 DEBUG 看上游原始响应，或者直接 curl 一次 Bedrock 确认。

**C.9 分工：骨架和 DEBUG 各管什么？**
骨架常开，用来证明客户的请求本身能不能被缓存，以及没命中时责任在哪一方，可以作为证据。客户需要进一步优化命中率时，再按需开 DEBUG 看正文，协助分析（2026-10-31 确认）。



## 附录 D：会话缓存诊断怎么看（面向使用者）

入口：使用日志 → 会话视图 → 展开某个会话，顶部"缓存诊断"（仅管理员，日志库为 ClickHouse 且该会话有骨架时才显示）。只有骨架采集上线后记录的请求才可分析；已保存的旧骨架会按当前规则重算，无须重新采集。

### D.1 汇总行

一个会话按 lane（对话轨道）分别推算：主对话、各 subagent、小的辅助调用会分成不同 lane。汇总采用 token 加权；Claude 展示实际、现有断点、尾部修正以及独立滚动推进策略，不能将它们读成保证递增的收益阶梯。

基础四项命中率，Claude 和 OpenAI 档名不同：

- **实际命中率**：上游真实返回的缓存命中 token 占比。两家一致。
- 第二档：Claude = **客户端断点上限**（按客户端现有断点能命中的上限）；OpenAI = **前缀匹配上限**（缓存按当前 prompt_cache_key 分区时的上限）。
- 第三档：Claude = **断点调整后**（网关把易变尾部断点移到稳定块后的上限）；OpenAI = **统一 prompt_cache_key 后**（整个会话用同一个 key，去掉按 key 分区影响）。
- 第四档：Claude = **调整 + 1h TTL**；OpenAI = **统一 key + 24h 保留**。在第三档基础上再放宽保留时间。注意 Azure 不支持 `prompt_cache_retention`，这一档对 Azure 只作参考（界面有悬停提示）。

Claude 另有 **推进**（`advanced`）与 **推进 1h**（`advanced_1h`），对应逐轮 `expected_advanced` / `expected_advanced_1h`：模拟逐轮推进可缓存端点并保留上一轮稳定端点的策略，遵守最多 4 个端点、20 block 回看、TTL 和响应完成后写入可见。它们各有独立历史，须与尾部修正（`adjusted` / `adjusted1h`）分别比较；不能把推进理解成“尾部修正再加一档”或无限回看。OpenAI 不展示这两项。

读法：模拟读取高于实际，表示该假设值得核查；独立策略不保证相邻数值递增，差额不是保证节省量。各项持平也不能证明已达到真实上游最优。

概览中的损失差额仅用于比较假设，不等同于完整未缓存分解。Claude 完整分解按 §7.6.4 分配，包括首轮、新内容、覆盖不足及原因未明：

- **上游损失**：现有断点模拟可读而实际未读的差额，包含 `channel_switch` 与 `upstream_miss`；切换仅为相关信号，同渠道/key 也不能直接证明上游内部故障。
- **断点位置不当**（Claude）：尾部修正模拟的增量；OpenAI 对应 **prompt_cache_key 变化**。都不是线上必然收益。
- **TTL 过期**：放宽保留时间模拟增加的读取部分，仍受实际写入等条件约束。
- **断点覆盖不足 / stalled**（`breakpoint_stalled`）：稳定旧历史超出前后两轮最远断点，反映写覆盖不足；不保证这些 token 在本轮仍可回收。
- **稳定前缀原因未明 / unknown**（`stable_unexplained`）：稳定旧前缀尚无足够证据解释未命中，既不算新增内容，也不能直接算上游责任。
- **影响轮次**：同一轮同一原因即使分多段也只计一次；不同原因可覆盖同一轮，不能把各原因轮次相加当请求总量。各原因 token 则是不重叠分区。

### D.2 逐轮列表（"展开轮次"）

每一行是会话里的一轮请求，列含义：

- **时间 / 间隔**：请求时刻，以及距上一轮结束的秒数。间隔大不一定导致掉缓存，要看原因是否真判为 TTL 过期。
- **提示词前缀**（彩条）：本轮提示词按块画成一条，宽度按 lane 内最大请求缩放，颜色是与上一轮相比：灰 = 相同前缀（不证明已写入或仍可读），红 = 相对上一轮变化的块，绿 = 本轮新增块（通常是末尾新的一问一答），竖线 = 断点位置。增量对话通常是"一长条灰 + 尾部一点绿"；仍需检查灰色旧历史是否被断点覆盖。红色出现在前缀靠前的位置，说明缓存前缀被破坏。
- **实际 / 预期**：本轮实际命中率与模拟预期命中率。两者差得多，就是异常轮。
- **原因**（标签）：按固定优先级检查首轮、低于缓存下限、无断点、参数变化、工具或系统提示变化、与上一轮并发；其后选择超过本轮 5% 的最大候选。Claude 候选采用同一 token 分解（排除 `new_content`），包含覆盖不足、原因未明等；标签不代替完整分解，数量分解本身没有 5% 截断。OpenAI 沿用原候选，另有"未带 prompt_cache_key"（灰字提示）和"使用 previous_response_id"（前缀在上游、本轮不推算，按实际计入）。
- 点某一轮的"骨架"按钮可打开该轮请求骨架，逐块看哈希、大小与断点。

### D.3 典型判读

- **前缀几乎全灰但实际命中低**：先看断点是否长期留在早期位置；有稳定历史超出两轮端点时检查覆盖不足。没有足够模拟写入/读取证据的稳定差额看原因未明。只有当前策略模拟可读而实际未读的部分才归上游相关差额；结合渠道/key、上游路由及驱逐记录核查，不能只凭灰色前缀锁定账号池波动。
- **红色块出现在前缀靠前**：被标为"工具或系统提示变化"或"参数变化"，是客户端请求结构每轮在变（tools 顺序、system 带动态值、effort 变化等），缓存前缀被破坏，需要客户端侧调整。
- **"断点位置不当"（Claude）占损失大头**：检查易变尾部与尾部修正模拟；覆盖不足则另看推进 / 推进 1h。开启 `cache_breakpoint_fix_setting` 后仍需用真实 usage 验证收益，模拟结果不会自动变成实际值。
- **"prompt_cache_key 变化 / 未带 prompt_cache_key"（OpenAI）占大头**：检查 key 稳定性及 `prompt_cache_key_injection_setting`，以真实流量验证；统一 key 模拟不保证实际命中。
