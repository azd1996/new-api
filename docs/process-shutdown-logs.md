# 进程停机中断日志

收到 SIGTERM/SIGINT 后，服务先停止接收新的转发请求，在
`SHUTDOWN_TIMEOUT_SECONDS`（默认 120 秒）内等待现有请求完成。
本次接入范围为普通 HTTP/SSE 转发，以及 AWS Bedrock SDK 调用。

等待超时后，仍在执行的已登记转发请求会被标记为 `shutdown_timeout`，取消已接入的上游请求，
关闭下游 HTTP 连接，进入现有日志及结算收尾流程。
正常等待期内完成的请求不带停机标记。

应用随后最多等待 `SHUTDOWN_CLEANUP_TIMEOUT_SECONDS`（默认 60 秒）让请求收尾，
再保存看板缓存、关闭日志 shipper 和数据库。该值必须为正数，非正数回退为 60 秒。
外部容器的停止等待时间应覆盖正常等待、收尾等待和其他退出操作。
客户端自行断连不触发停机标记，也不会因为本功能改变原有上游 usage 收集行为。

## 持久化与详情

沿用消耗日志（type=2）和错误日志（type=5），不新增表或日志类型。
标记保存在 `other.admin_info.process_shutdown`：

```json
{
  "instance": "node-a",
  "batch_id": "本次停机的唯一ID",
  "at": 1792886400,
  "reason": "shutdown_timeout"
}
```

`at` 为强制取消时的 Unix 秒时间戳，同一次停机的请求共享批次 ID。
管理员可在两类日志的详情中查看原因、实例、批次和时间。
普通用户查询会沿用现有逻辑剥离 `admin_info`。
`other` 会随日志 shipper 同步到远端，无需修改远端表结构。

## 查询统计

可在日志数据库按停机批次统计不同下游请求数。以下查询适用于 SQLite、MySQL 和
PostgreSQL；`?` 表示查询工具的绑定参数，具体占位符语法按客户端调整：

```sql
SELECT COUNT(DISTINCT request_id) AS interrupted_requests
FROM logs
WHERE type IN (2, 5)
  AND created_at >= ?
  AND created_at <= ?
  AND other LIKE ?;
```

最后一个参数为 `%"batch_id":"实际批次ID"%`。批次 ID 为单次进程停机生成，
因此也对应详情中的实例。限定时间范围可减少对 `other` 文本的扫描。
使用 `DISTINCT request_id` 避免同一请求同时存在消耗和错误记录时重复计数。
此查询针对应用生成的紧凑 JSON，不适用于手工重新格式化后的 JSON 文本。

## 范围

记录的是停机强制取消时仍未完成的转发请求，并非客户端确认收到的失败数。
依赖现有消耗、错误日志开关和正常写入链路，不补记额外日志。
SIGKILL、OOM、掉电、数据库长期不可用，以及收尾期内仍未返回的请求，无法保证落库。
保留 Anthropic、OpenAI、Azure 的公共 HTTP/SSE 链路、AWS Bedrock SDK，
以及 Gemini/Vertex 普通请求和 Vertex 普通认证请求的停机取消接入。
Realtime WebSocket、视频任务、Midjourney、Suno 等专用路由不纳入本次登记。
其他渠道不再单独适配；经公共 HTTP/SSE 函数的调用仍可共享取消能力，
但其专用 SDK、WebSocket、上传或轮询流程不保证及时取消及落库。
