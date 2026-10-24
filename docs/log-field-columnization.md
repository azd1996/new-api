# 日志 other 字段「提列」方案文档

## 1. 背景与目标

模型访问的消耗日志、错误日志里，近一个月新加的字段(session_id、trace-tag、
customer_id、统计金额、error_signature_hash 等)都为了规避「改表牵动上下游」而
统一塞进了 `other` JSON。随着字段变多，按这些值做过滤 / 分组 / 求和只能走
`other LIKE` 或 Go 内存解析，查询效率和可维护性都在下降。

目标：把高价值字段从 `other` JSON 逐步「提升」为本机 `logs` 表的真列，同时
**完全不惊动远端 ClickHouse 集群那条长链路**。

## 2. 数据流现状(务必先理解)

- 写：`model.createLog` 先 `LOG_DB.Create(log)` 实时写**本机**日志库(单机
  ClickHouse / MySQL / SQLite)，成功后 `shipLog` 把同一行按 `logshipper.Row`
  的列名落到本地文件，经 LoongCollector → Kafka → Flink → **远端 ClickHouse 集群**。
- 读：管理台的消耗 / 错误日志、错误统计、RPM/TPM/额度汇总全部读**本机** `LOG_DB`；
  仅「小时聚合账单」在开 `CSL_HOURLY_READER_ENABLED` 时才走问渠 OmniDataSearch。
- 两套 schema 相互独立：本机 `logs` 表由手写 DDL `clickHouseLogCreateTableSQL`
  (`model/main.go`)管理，列名 = `model.Log` 字段；远端集群表是另一套列名
  (`ts`/`channel_id`/`group_name`…)，靠 `logshipper.Row` 的 json tag 对齐。

结论：本机表 schema 完全归我们掌控，加真列只动本机这套，远端零改动。

## 3. 方案：双写(真列 + 保留 other)，读暂不切

分两期，逐期独立可交付：

- **一期**：新增真列，写入时**真列和 `other` 都写**；读路径不动(仍解析 `other`)。
  新老行都在 `other` 里，统计无缺口，不需要任何兼容分支或 backfill。
- **二期(可选、将来需要时)**：把读从 `other` JSON 切到列(SQL `GROUP BY` /
  列谓词)。此时只在 `other` 里的老行会从「按列查询」中消失——若配了 TTL 且已过期
  则无影响；若 TTL=0(默认永久保留)可做一次性 backfill 或直接忽略少量老数据。

## 4. 首个落地字段：error_signature_hash

错误日志的归一化错误指纹，原先只在 `other.error_signature_hash`。一期改造(已落地)：

- `model.Log` 新增 `ErrorSignatureHash string`(`varchar(64)`,
  `json:"error_signature_hash,omitempty"`，仅错误日志有值)。
- `RecordErrorLog` 从 `other["error_signature_hash"]` 取值同时写入新列，**保留**
  `other` 里那份(nil map 读取安全，返回 `""`)。
- 本机 ClickHouse：建表 DDL 加 `error_signature_hash String DEFAULT ''`；
  `migrateClickHouseLogDB` 加幂等 `ALTER TABLE logs ADD COLUMN IF NOT EXISTS`
  给存量实例补列。
- MySQL / SQLite：`migrateLOGDB` 走 `AutoMigrate(&Log{})`，结构体新字段自动补列。
- 读路径未动：`model/error_stats.go`、`GetAllLogs` 的 `errorSignatureHash` 过滤
  仍基于 `other`，故新老行都正常。

## 4b. 第二批字段：原始/折扣金额 5 列(消耗日志)

把结算时算出的「仅统计」金额从 `other` 提升为本机真列(一期同款双写)：

- `model.Log` 新增 `RawQuota int`、`StatQuota int`、`StatGroupRatio float64`、
  `CustomerStatQuota int`、`CustomerDiscount float64`(`json ...,omitempty`、`default:0`)。
- `RecordConsumeLog` 从 `params.Other` 取 `raw_quota`/`stat_quota`/`stat_group_ratio`/
  `customer_stat_quota`/`customer_discount` 并写入对应列，**保留** `other` 那份
  (nil/缺键读取安全得零值;`customer_*` 仅在请求带客户 id 时才有)。源头类型见
  `service/text_quota.go` 的 `textQuotaSummary`(两个金额 `int`、两个比率 `float64`)。
- 本机 ClickHouse：建表 DDL 加 `raw_quota/stat_quota/customer_stat_quota Int32`、
  `stat_group_ratio/customer_discount Float64`(都 `DEFAULT 0`)；`migrateClickHouseLogDB`
  用一个 `ADD COLUMN IF NOT EXISTS` 列表循环给存量实例逐列补齐。
- MySQL / SQLite：`AutoMigrate(&Log{})` 自动补列。
- 读路径(二期已切列)：金额统计 `SumLogStatAmounts` 改为按列 `SUM`——
  `COALESCE(SUM(stat_quota),0)` / `SUM(customer_stat_quota)` / 利润用
  `SUM(CASE WHEN customer_discount>0 THEN customer_stat_quota-stat_quota ELSE 0 END)`
  (`customer_discount>0` 作「有客户」判据，等价于旧逻辑按 `customer_stat_quota` 键是否存在)。
  去掉了原先 10 万行扫描上限，改为 DB 聚合全量。**注意:列加上之前的历史消耗日志**
  金额只在 `other`、列为空(0)，按列 `SUM` 会把它们计 0；新日志不受影响。详情弹窗仍读 `other`。

## 5. 关键决策：加真列后，不把字段从 `other` 移除

**决策**：把某字段提升为本机真列后，**仍保留它在 `other` JSON 里的那份**，即长期
「真列 + `other`」双份存储，不做二期的「停写 `other`」。

**理由**：

- 会被提成真列的字段本身都很短(hash、id、枚举、金额数值)，真列与 `other` 存双份的
  额外存储 / 写入开销很小，可以接受。
- 字段继续留在 `other`，就会**无缝随 `other` 继续同步到 `logshipper.Row` → 远端
  ClickHouse 集群**。因为 `logshipper.Row` 的字段白名单里已经有 `other`，不需要给
  远端集群表加列、不需要改 ck-ingest 契约 / Flink，远端那条长链路彻底零改动。
- 读路径老行天然兼容(老行只有 `other` 也能被查到)，无需 backfill、无需
  `col=? OR other LIKE ?` 这种会让索引失效的双读兼容查询。

**取舍**：真列主要服务**本机**的高效过滤 / 分组 / 求和；`other` 那份服务**远端集群
及历史兼容**。两者定位不同，双份是刻意为之，不是冗余待清理项。

**适用范围**：本决策适用于后续所有「`other` → 本机真列」的字段提升(customer_id、
session_id、统计金额等)，除非某字段**明确不需要**进远端集群、且体积较大，才考虑
二期停写 `other`。

## 6. 新增一个「提列」字段的标准步骤(复用清单)

1. `model.Log`(`model/log.go`)加字段 + json/gorm tag(`default:''` / `default:0`)。
2. 本机 ClickHouse 建表 DDL `clickHouseLogCreateTableSQL`(`model/main.go`)加列。
3. `migrateClickHouseLogDB` 加幂等 `ALTER TABLE logs ADD COLUMN IF NOT EXISTS ...`
   给存量 CH 补列；**先迁列、后写值**，否则 GORM INSERT 带未知列会失败。
4. MySQL / SQLite 无需手动迁移,`AutoMigrate(&Log{})` 自动补列。
5. 写入处(`RecordConsumeLog` / `RecordErrorLog` 等)在**保留 `other`** 的同时给
   新列赋值。
6. **不要**把新列加进 `logshipper.Row`——远端集群不受影响；字段靠 `other` 继续同步。
7. 读路径默认不动(二期才切列)。CH 与 MySQL 两种日志库各验证一遍。

## 7. 要避免的反模式

- 别做 `WHERE col = ? OR other LIKE ?` 双读兼容：JSON LIKE 让索引失效，抵消列化收益。
- 别为少量老数据在 ClickHouse 上做 `ALTER TABLE ... UPDATE` 重 mutation，除非确有必要。
- 别把提列字段加进 `logshipper.Row` / 远端集群表,会重新引入跨团队 schema 协调成本。

