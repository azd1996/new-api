import {
  Alert02Icon,
  CheckmarkCircle02Icon,
  Clock01Icon,
  Coins01Icon,
  Database01Icon,
  Refresh01Icon,
  ServerStack01Icon,
  Timer02Icon,
  ZapIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { cn } from '@/lib/utils'

import { MOCK_USAGE_LOG_STATISTICS } from '../statistics-mock'
import type {
  ChannelStatistic,
  LatencyStatistic,
  StatisticsJobStatus,
  StatusStatistic,
} from '../statistics-types'
import { useUsageLogsContext } from './usage-logs-provider'

function formatInteger(value: number): string {
  return new Intl.NumberFormat('en-US').format(value)
}

function formatAmount(value: number, sensitiveVisible: boolean): string {
  if (!sensitiveVisible) return '••••'
  return `$${value.toLocaleString('en-US', {
    minimumFractionDigits: 3,
    maximumFractionDigits: 3,
  })}`
}

function formatPercent(value: number): string {
  return `${value.toFixed(2)}%`
}

function formatSeconds(value: number): string {
  return `${value.toFixed(3)}s`
}

function getStatusBadge(status: StatisticsJobStatus, label: string) {
  const statusStyles: Record<StatisticsJobStatus, string> = {
    completed:
      'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
    running: 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300',
    paused:
      'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300',
    failed: 'border-destructive/30 bg-destructive/10 text-destructive',
  }

  return (
    <Badge variant='outline' className={cn('font-normal', statusStyles[status])}>
      {label}
    </Badge>
  )
}

function MetricCard(props: {
  label: string
  value: string
  description: string
  icon: typeof CheckmarkCircle02Icon
  accent: string
}) {
  return (
    <Card className='overflow-hidden'>
      <CardContent className='p-4'>
        <div className='flex items-start justify-between gap-3'>
          <div className='min-w-0'>
            <p className='text-muted-foreground truncate text-xs font-medium'>
              {props.label}
            </p>
            <p className='mt-2 text-2xl font-semibold tracking-tight tabular-nums'>
              {props.value}
            </p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {props.description}
            </p>
          </div>
          <span
            className={cn(
              'flex size-9 shrink-0 items-center justify-center rounded-lg',
              props.accent
            )}
          >
            <HugeiconsIcon icon={props.icon} size={18} strokeWidth={1.8} />
          </span>
        </div>
      </CardContent>
    </Card>
  )
}

function SectionHeading(props: { title: string; description: string }) {
  return (
    <div className='mb-4'>
      <h2 className='text-base font-semibold'>{props.title}</h2>
      <p className='text-muted-foreground mt-1 text-xs'>{props.description}</p>
    </div>
  )
}

function JobProgress() {
  const { t } = useTranslation()
  const job = MOCK_USAGE_LOG_STATISTICS.job
  const progress = Math.round((job.processedLogs / job.totalLogs) * 100)

  return (
    <Card className='border-primary/20 bg-primary/[0.03]'>
      <CardContent className='p-4'>
        <div className='flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between'>
          <div className='flex min-w-0 items-start gap-3'>
            <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
              <HugeiconsIcon icon={Database01Icon} size={18} />
            </span>
            <div className='min-w-0'>
              <div className='flex flex-wrap items-center gap-2'>
                <h2 className='font-semibold'>{t('Statistics job')}</h2>
                {getStatusBadge(job.status, t('Completed'))}
                <Badge variant='secondary' className='font-normal'>
                  {t('Mock data')}
                </Badge>
              </div>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t('Consume and error logs · all types · last 24 hours')}
              </p>
            </div>
          </div>
          <div className='text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-2 text-xs'>
            <span className='inline-flex items-center gap-1.5'>
              <HugeiconsIcon icon={Clock01Icon} size={14} />
              {t('Updated')} {job.updatedAt}
            </span>
            <span className='inline-flex items-center gap-1.5'>
              <HugeiconsIcon icon={Timer02Icon} size={14} />
              {t('Duration')} {job.duration}
            </span>
            <Button variant='outline' size='sm' className='h-8 gap-1.5'>
              <HugeiconsIcon icon={Refresh01Icon} size={14} />
              {t('Refresh')}
            </Button>
          </div>
        </div>
        <div className='mt-4'>
          <div className='mb-1.5 flex items-center justify-between text-xs'>
            <span className='text-muted-foreground'>
              {t('Processed logs')}
            </span>
            <span className='font-mono tabular-nums'>
              {formatInteger(job.processedLogs)} / {formatInteger(job.totalLogs)}
            </span>
          </div>
          <Progress value={progress} className='h-1.5' />
        </div>
      </CardContent>
    </Card>
  )
}

type ChannelSortKey =
  | 'requests'
  | 'attempts'
  | 'successRate'
  | 'retryRate'
  | 'rawAmount'
  | 'channelAmount'
  | 'customerAmount'
  | 'profit'
  | 'ttftP95'
  | 'e2eP95'
  | 'hitTokenRatio'
  | 'hitRequestRatio'

function getSortIndicator(
  sortKey: ChannelSortKey,
  columnKey: ChannelSortKey,
  direction: 'asc' | 'desc'
): string {
  if (sortKey !== columnKey) return '↕'
  return direction === 'asc' ? '↑' : '↓'
}

function ChannelComparisonTable(props: {
  rows: ChannelStatistic[]
  sensitiveVisible: boolean
}) {
  const { t } = useTranslation()
  const [sortKey, setSortKey] = useState<ChannelSortKey>('requests')
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('desc')

  const columns: Array<{
    key: ChannelSortKey
    label: string
    format: (value: number) => string
  }> = [
    { key: 'requests', label: t('Requests'), format: formatInteger },
    { key: 'attempts', label: t('Attempts'), format: formatInteger },
    {
      key: 'successRate',
      label: t('Success rate'),
      format: formatPercent,
    },
    { key: 'retryRate', label: t('Retry rate'), format: formatPercent },
    {
      key: 'rawAmount',
      label: t('Raw amount'),
      format: (value) => formatAmount(value, props.sensitiveVisible),
    },
    {
      key: 'channelAmount',
      label: t('Channel amount'),
      format: (value) => formatAmount(value, props.sensitiveVisible),
    },
    {
      key: 'customerAmount',
      label: t('Customer amount'),
      format: (value) => formatAmount(value, props.sensitiveVisible),
    },
    {
      key: 'profit',
      label: t('Profit'),
      format: (value) => formatAmount(value, props.sensitiveVisible),
    },
    { key: 'ttftP95', label: t('TTFT P95'), format: formatSeconds },
    { key: 'e2eP95', label: t('E2E P95'), format: formatSeconds },
    {
      key: 'hitTokenRatio',
      label: t('Hit token ratio'),
      format: formatPercent,
    },
    {
      key: 'hitRequestRatio',
      label: t('Hit request ratio'),
      format: formatPercent,
    },
  ]

  const sortedRows = useMemo(
    () =>
      [...props.rows].sort((left, right) => {
        const difference = left[sortKey] - right[sortKey]
        return sortDirection === 'asc' ? difference : -difference
      }),
    [props.rows, sortDirection, sortKey]
  )

  const handleSort = (key: ChannelSortKey) => {
    if (sortKey === key) {
      setSortDirection((current) => (current === 'asc' ? 'desc' : 'asc'))
      return
    }
    setSortKey(key)
    setSortDirection('desc')
  }

  return (
    <div className='overflow-x-auto rounded-lg border'>
      <table className='w-full min-w-[1420px] text-sm'>
        <thead className='bg-muted/40 text-muted-foreground text-xs'>
          <tr>
            <th className='px-3 py-2.5 text-left font-medium'>{t('Channel')}</th>
            {columns.map((column) => (
              <th key={column.key} className='px-3 py-0 text-right font-medium'>
                <button
                  type='button'
                  className='hover:text-foreground inline-flex h-9 items-center gap-1 whitespace-nowrap'
                  onClick={() => handleSort(column.key)}
                  aria-label={`${t('Sort by')} ${column.label}`}
                >
                  {column.label}
                  <span className='text-[10px]'>
                    {getSortIndicator(sortKey, column.key, sortDirection)}
                  </span>
                </button>
              </th>
            ))}
          </tr>
        </thead>
        <tbody className='divide-y'>
          {sortedRows.map((row) => (
            <tr key={row.name} className='hover:bg-muted/25'>
              <td className='px-3 py-3'>
                <div className='flex items-center gap-2'>
                  <span className='bg-primary/10 text-primary flex size-7 items-center justify-center rounded-md'>
                    <HugeiconsIcon icon={ServerStack01Icon} size={14} />
                  </span>
                  <span className='font-medium'>{row.name}</span>
                </div>
              </td>
              {columns.map((column) => (
                <td
                  key={`${row.name}-${column.key}`}
                  className={cn(
                    'px-3 py-3 text-right font-mono tabular-nums',
                    column.key === 'profit' &&
                      'text-emerald-600 dark:text-emerald-400'
                  )}
                >
                  {column.format(row[column.key])}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function StatusDistribution(props: { rows: StatusStatistic[] }) {
  const { t } = useTranslation()
  const toneStyles: Record<StatusStatistic['tone'], string> = {
    success: 'bg-emerald-500',
    warning: 'bg-amber-500',
    error: 'bg-rose-500',
    neutral: 'bg-slate-400',
  }

  return (
    <div className='space-y-3'>
      {props.rows.map((row) => (
        <div key={row.code}>
          <div className='mb-1.5 flex items-center justify-between text-xs'>
            <span className='flex items-center gap-2'>
              <span className='font-mono font-medium'>{row.code}</span>
              <span className='text-muted-foreground'>
                {formatInteger(row.count)} {t('requests')}
              </span>
            </span>
            <span className='font-mono tabular-nums'>
              {formatPercent(row.percentage)}
            </span>
          </div>
          <div className='bg-muted h-2 overflow-hidden rounded-full'>
            <div
              className={cn('h-full rounded-full', toneStyles[row.tone])}
              style={{ width: `${Math.max(row.percentage, 0.5)}%` }}
            />
          </div>
        </div>
      ))}
    </div>
  )
}

function LatencyTable(props: { rows: LatencyStatistic[] }) {
  const { t } = useTranslation()

  return (
    <div className='overflow-x-auto rounded-lg border'>
      <table className='w-full min-w-[840px] text-sm'>
        <thead className='bg-muted/40 text-muted-foreground text-xs'>
          <tr>
            <th className='px-3 py-2.5 text-left font-medium'>{t('Metric')}</th>
            <th className='px-3 py-2.5 text-right font-medium'>{t('Samples')}</th>
            <th className='px-3 py-2.5 text-right font-medium'>{t('Missing')}</th>
            {[
              ['mean', 'Mean'],
              ['p50', 'P50'],
              ['p90', 'P90'],
              ['p95', 'P95'],
              ['p99', 'P99'],
              ['max', 'MAX'],
            ].map(([key, label]) => (
              <th key={key} className='px-3 py-2.5 text-right font-medium'>
                {label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className='divide-y'>
          {props.rows.map((row) => (
            <tr key={row.metric}>
              <td className='px-3 py-3 font-medium'>{row.metric}</td>
              <td className='px-3 py-3 text-right font-mono tabular-nums'>
                {formatInteger(row.sampleCount)}
              </td>
              <td className='text-muted-foreground px-3 py-3 text-right font-mono tabular-nums'>
                {formatInteger(row.missingCount)}
              </td>
                {[
                  ['mean', row.mean],
                  ['p50', row.p50],
                  ['p90', row.p90],
                  ['p95', row.p95],
                  ['p99', row.p99],
                  ['max', row.max],
                ].map(([key, value]) => (
                  <td
                    key={`${row.metric}-${key}`}
                    className='px-3 py-3 text-right font-mono tabular-nums'
                  >
                    {formatSeconds(Number(value))}
                  </td>
                ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function CacheMetric(props: { label: string; value: number; description: string }) {
  return (
    <div className='bg-muted/25 rounded-lg border p-3'>
      <p className='text-muted-foreground text-xs'>{props.label}</p>
      <p className='mt-1 text-xl font-semibold tabular-nums'>
        {formatPercent(props.value)}
      </p>
      <p className='text-muted-foreground mt-1 text-[11px] leading-4'>
        {props.description}
      </p>
    </div>
  )
}

export function UsageLogsStatistics() {
  const { t } = useTranslation()
  const { sensitiveVisible } = useUsageLogsContext()
  const statistics = MOCK_USAGE_LOG_STATISTICS
  const overview = statistics.overview
  const cache = statistics.cache
  const cacheDetails = useMemo(
    () => [
      {
        label: t('Hit token ratio'),
        value: cache.hitTokenRatio,
        description: t('Hit tokens / input tokens'),
      },
      {
        label: t('Hit or created token ratio'),
        value: cache.hitOrCreatedTokenRatio,
        description: t('(Hit + created tokens) / input tokens'),
      },
      {
        label: t('Hit request ratio'),
        value: cache.hitRequestRatio,
        description: t('Hit requests / unique requests'),
      },
      {
        label: t('Hit or created request ratio'),
        value: cache.hitOrCreatedRequestRatio,
        description: t('(Hit + created requests) / unique requests'),
      },
    ],
    [cache, t]
  )

  return (
    <div className='space-y-4 overflow-auto pb-6'>
      <JobProgress />

      <Card>
        <CardHeader className='pb-3'>
          <SectionHeading
            title={t('Channel comparison')}
            description={t(
              'Compare channel traffic, amounts, latency, and cache metrics. Click a column to sort.'
            )}
          />
        </CardHeader>
        <CardContent className='pt-0'>
          <ChannelComparisonTable
            rows={statistics.channels}
            sensitiveVisible={sensitiveVisible}
          />
        </CardContent>
      </Card>

      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        <MetricCard
          label={t('Unique requests')}
          value={formatInteger(overview.uniqueRequests)}
          description={t('Deduplicated by request ID')}
          icon={ZapIcon}
          accent='bg-indigo-500/10 text-indigo-600 dark:text-indigo-300'
        />
        <MetricCard
          label={t('Success rate')}
          value={formatPercent(overview.successRate)}
          description={`${formatInteger(overview.errorRequests)} ${t('failed requests')}`}
          icon={CheckmarkCircle02Icon}
          accent='bg-emerald-500/10 text-emerald-600 dark:text-emerald-300'
        />
        <MetricCard
          label={t('Retry rate')}
          value={formatPercent(overview.retryRate)}
          description={t('Requests with more than one attempt')}
          icon={Refresh01Icon}
          accent='bg-amber-500/10 text-amber-600 dark:text-amber-300'
        />
        <MetricCard
          label={t('Total amount')}
          value={formatAmount(overview.totalAmount, sensitiveVisible)}
          description={`${formatInteger(overview.inputTokens)} ${t('input tokens')}`}
          icon={Coins01Icon}
          accent='bg-violet-500/10 text-violet-600 dark:text-violet-300'
        />
      </div>

      <div className='grid gap-4 xl:grid-cols-[minmax(0,1.65fr)_minmax(280px,0.75fr)]'>
        <Card>
          <CardHeader className='pb-3'>
            <SectionHeading
              title={t('Final status distribution')}
              description={t('Calculated from each request final result')}
            />
          </CardHeader>
          <CardContent className='pt-0'>
            <StatusDistribution rows={statistics.statuses} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader className='pb-3'>
            <SectionHeading
              title={t('Channel comparison guide')}
              description={t('How to read the comparison table')}
            />
          </CardHeader>
          <CardContent className='text-muted-foreground space-y-3 pt-0 text-xs leading-5'>
            <p>{t('Requests are unique request IDs; attempts include retries.')}</p>
            <p>{t('Amount columns use USD and show up to three decimals.')}</p>
            <p>{t('Latency columns use seconds and show P95 values.')}</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className='pb-3'>
          <SectionHeading
            title={t('Latency distribution')}
            description={t(
              'Seconds, with millisecond precision. Missing fields are excluded from the distribution.'
            )}
          />
        </CardHeader>
        <CardContent className='pt-0'>
          <LatencyTable rows={statistics.latency} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className='pb-3'>
          <SectionHeading
            title={t('Cache coverage')}
            description={t(
              'Request denominators use all unique requests. Cache eligibility is not inferred.'
            )}
          />
        </CardHeader>
        <CardContent className='space-y-4 pt-0'>
          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            {cacheDetails.map((item) => (
              <CacheMetric key={item.label} {...item} />
            ))}
          </div>
          <div className='text-muted-foreground grid gap-2 border-t pt-3 text-xs sm:grid-cols-2 xl:grid-cols-4'>
            <span>
              {t('Hit tokens')}{' '}
              <strong className='text-foreground font-mono'>
                {formatInteger(cache.hitTokens)}
              </strong>
            </span>
            <span>
              {t('Created tokens')}{' '}
              <strong className='text-foreground font-mono'>
                {formatInteger(cache.createdTokens)}
              </strong>
            </span>
            <span>
              {t('Hit requests')}{' '}
              <strong className='text-foreground font-mono'>
                {formatInteger(cache.hitRequests)}
              </strong>
            </span>
            <span>
              {t('Created requests')}{' '}
              <strong className='text-foreground font-mono'>
                {formatInteger(cache.createdRequests)}
              </strong>
            </span>
          </div>
        </CardContent>
      </Card>

      <div className='border-amber-500/30 bg-amber-500/[0.06] flex items-start gap-3 rounded-lg border p-3 text-sm'>
        <HugeiconsIcon
          icon={Alert02Icon}
          size={18}
          className='text-amber-600 mt-0.5 shrink-0 dark:text-amber-300'
        />
        <div>
          <p className='font-medium'>{t('Data quality notice')}</p>
          <p className='text-muted-foreground mt-1 text-xs leading-5'>
            {t(
              'Some requests contain multiple consume logs. They are fully accumulated and marked as anomalies.'
            )}{' '}
            <span className='font-mono'>
              {formatInteger(statistics.duplicateConsumeRequests)}
            </span>
          </p>
        </div>
      </div>
    </div>
  )
}
