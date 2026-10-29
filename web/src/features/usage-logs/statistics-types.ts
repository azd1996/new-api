export type StatisticsJobStatus = 'completed' | 'running' | 'paused' | 'failed'

export type StatisticsJob = {
  status: StatisticsJobStatus
  processedLogs: number
  totalLogs: number
  processedHours: number
  totalHours: number
  updatedAt: string
  duration: string
}

export type StatisticsOverview = {
  uniqueRequests: number
  successRate: number
  retryRate: number
  inputTokens: number
  totalAmount: number
  errorRequests: number
}

export type ChannelStatistic = {
  name: string
  requests: number
  attempts: number
  successRate: number
  retryRate: number
  rawAmount: number
  channelAmount: number
  customerAmount: number
  profit: number
  ttftP95: number
  e2eP95: number
  hitTokenRatio: number
  hitRequestRatio: number
}

export type StatusStatistic = {
  code: string
  count: number
  percentage: number
  tone: 'success' | 'warning' | 'error' | 'neutral'
}

export type LatencyStatistic = {
  metric: string
  sampleCount: number
  missingCount: number
  mean: number
  p50: number
  p90: number
  p95: number
  p99: number
  max: number
}

export type CacheStatistic = {
  hitTokenRatio: number
  hitOrCreatedTokenRatio: number
  hitRequestRatio: number
  hitOrCreatedRequestRatio: number
  hitTokens: number
  createdTokens: number
  inputTokens: number
  hitRequests: number
  createdRequests: number
  uniqueRequests: number
}

export type UsageLogStatistics = {
  job: StatisticsJob
  overview: StatisticsOverview
  channels: ChannelStatistic[]
  statuses: StatusStatistic[]
  latency: LatencyStatistic[]
  cache: CacheStatistic
  duplicateConsumeRequests: number
}
