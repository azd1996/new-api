/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import { formatLogQuota, formatNumber, formatPercent } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getLogStats, getUserLogStats } from '../api'
import { DEFAULT_LOG_STATS } from '../constants'
import { buildApiParams } from '../lib/utils'
import type { LogStatistics } from '../types'
import { useLogsViewScope, useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

function StatBadge(props: {
  label: string
  value: string | number
  accent: string
}) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}

export function CommonLogsStats() {
  const { t, i18n } = useTranslation()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const searchParams = route.useSearch()
  const { sensitiveVisible } = useUsageLogsContext()

  // Defer the stats query until the browser is idle after first paint so the
  // log list renders first; the stats aggregate scans the whole time range and
  // should not compete with the list request on initial open.
  const [deferred, setDeferred] = useState(false)
  useEffect(() => {
    const idle = window.requestIdleCallback
    if (idle) {
      const handle = idle(() => setDeferred(true), { timeout: 1500 })
      return () => window.cancelIdleCallback?.(handle)
    }
    const timer = window.setTimeout(() => setDeferred(true), 300)
    return () => window.clearTimeout(timer)
  }, [])

  const { data: stats } = useQuery({
    queryKey: ['usage-logs-stats', isAdmin, searchParams],
    enabled: deferred,
    queryFn: async () => {
      const params = buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams,
        columnFilters: [],
        isAdmin,
      })

      const result = isAdmin
        ? requireServerSuccess(await getLogStats(params))
        : requireServerSuccess(await getUserLogStats(params))

      return result.success
        ? result.data || DEFAULT_LOG_STATS
        : DEFAULT_LOG_STATS
    },
    placeholderData: (previousData) => previousData,
  })

  if (!stats) {
    return (
      <div className='flex items-center gap-2'>
        <Skeleton className='h-7 w-[150px] rounded-md' />
        <Skeleton className='h-7 w-[100px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
      </div>
    )
  }

  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)

  return (
    <div className='flex flex-col gap-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <StatBadge
          label={t('Usage')}
          value={sensitiveVisible ? formatLogQuota(stats?.quota || 0) : '••••'}
          accent='bg-sky-500/70'
        />
        <StatBadge
          label={t('RPM')}
          value={stats?.rpm || 0}
          accent='bg-rose-500/65'
        />
        <StatBadge
          label={t('TPM')}
          value={stats?.tpm || 0}
          accent='bg-slate-400/70'
        />
      </div>
      {isAdmin && <CommonLogsErrorRateRow stats={stats} locale={locale} />}
    </div>
  )
}

// CommonLogsErrorRateRow derives the "what-if" error stats from the raw outcome
// counts: actual requests (consume + error - retry-override), the original error
// count if neither override existed (error + param-override), and the actual
// error count (error - retry-override). The two rates divide each by actual
// requests. The param-override count only covers consume logs, mirroring the
// audit written to each consume log.
function CommonLogsErrorRateRow(props: {
  stats: LogStatistics
  locale: Intl.LocalesArgument
}) {
  const { t } = useTranslation()

  const consume = props.stats.consume || 0
  const errorCount = props.stats.error || 0
  const retryOverride = props.stats.retry_override || 0
  const paramOverride = props.stats.param_override || 0

  const actualRequests = Math.max(consume + errorCount - retryOverride, 0)
  const originalErrors = errorCount + paramOverride
  const actualErrors = Math.max(errorCount - retryOverride, 0)
  const originalRate =
    actualRequests > 0 ? (originalErrors / actualRequests) * 100 : 0
  const actualRate =
    actualRequests > 0 ? (actualErrors / actualRequests) * 100 : 0

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <StatBadge
        label={t('Actual Requests')}
        value={formatNumber(actualRequests, props.locale)}
        accent='bg-emerald-500/70'
      />
      <StatBadge
        label={t('Original Errors')}
        value={formatNumber(originalErrors, props.locale)}
        accent='bg-amber-500/70'
      />
      <StatBadge
        label={t('Actual Errors')}
        value={formatNumber(actualErrors, props.locale)}
        accent='bg-red-500/70'
      />
      <StatBadge
        label={t('Original Error Rate')}
        value={formatPercent(originalRate)}
        accent='bg-amber-500/70'
      />
      <StatBadge
        label={t('Actual Error Rate')}
        value={formatPercent(actualRate)}
        accent='bg-red-500/70'
      />
    </div>
  )
}
