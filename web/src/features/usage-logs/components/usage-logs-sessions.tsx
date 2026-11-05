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
import type { ColumnDef, OnChangeFn, SortingState } from '@tanstack/react-table'
import { Fragment, useCallback, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { TableCell, TableRow } from '@/components/ui/table'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { getLogSessions, getSessionCacheRangeAnalysis } from '../api'
import { skeletonRate, skeletonTierLabels } from '../lib/cache-skeleton'
import { buildApiParams } from '../lib/utils'
import type { LogSession, LogSessionSortBy } from '../types'
import { ByteCacheCoverageSummary } from './cache-coverage-summary'
import { CacheLossBreakdown } from './cache-loss-breakdown'
import { useSessionColumns } from './columns/session-columns'
import { SessionCacheDiagnosis } from './session-cache-diagnosis'
import { SessionRequestsTable } from './session-requests-table'
import { useLogsViewScope, useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const DEFAULT_SESSIONS_DATA = {
  enabled: true,
  items: [] as LogSession[],
  total: 0,
  total_tokens_sum: 0,
  range_total_tokens_sum: 0,
}

const SESSION_SORTABLE_COLUMNS = new Set<LogSessionSortBy>([
  'total_tokens',
  'range_total_tokens',
])

interface UsageLogsSessionsProps {
  toolbar: ReactNode
}

// UsageLogsSessions is the session view of the common logs: one row per
// session_id, expanded in place to list the session's requests.
export function UsageLogsSessions(props: UsageLogsSessionsProps) {
  const { t } = useTranslation()
  const { isAdminView, viewAccess } = useLogsViewScope()
  const { sensitiveVisible } = useUsageLogsContext()
  const searchParams = route.useSearch()
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [sorting, setSorting] = useState<SortingState>([])

  const toggleSession = useCallback((sessionId: string) => {
    setExpanded((previous) => {
      const next = new Set(previous)
      if (next.has(sessionId)) {
        next.delete(sessionId)
      } else {
        next.add(sessionId)
      }
      return next
    })
  }, [])

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search: searchParams,
      navigate: route.useNavigate(),
      pagination: { defaultPage: 1, defaultPageSize: 20 },
      globalFilter: { enabled: false },
    })

  const sortParams = useMemo(() => {
    const activeSort = sorting[0]
    if (
      !activeSort ||
      !SESSION_SORTABLE_COLUMNS.has(activeSort.id as LogSessionSortBy)
    ) {
      return {}
    }
    return {
      sort_by: activeSort.id as LogSessionSortBy,
      sort_order: activeSort.desc ? ('desc' as const) : ('asc' as const),
    }
  }, [sorting])

  const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
    setSorting(updater)
    if (pagination.pageIndex > 0) {
      onPaginationChange({ ...pagination, pageIndex: 0 })
    }
  }

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'logs',
      'sessions',
      viewAccess,
      pagination.pageIndex + 1,
      pagination.pageSize,
      searchParams,
      sortParams,
    ],
    queryFn: async () => {
      const params = buildApiParams({
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams,
        isAdmin: isAdminView,
      })
      const result = await getLogSessions(
        { ...params, ...sortParams },
        isAdminView
      )
      if (!result?.success) {
        throw createServerError(result, t('Failed to load sessions'))
      }
      return result.data || DEFAULT_SESSIONS_DATA
    },
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[2] === viewAccess ? previousData : undefined,
  })

  const sessionsEnabled = data?.enabled ?? true
  const cacheSummaryQuery = useQuery({
    queryKey: ['logs', 'sessions', 'cache-summary', viewAccess, searchParams],
    queryFn: async () => {
      const params = buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams,
        isAdmin: true,
      })
      const result = await getSessionCacheRangeAnalysis(params)
      if (!result?.success) {
        throw createServerError(result, t('Failed to load sessions'))
      }
      return result.data
    },
    enabled: isAdminView && sessionsEnabled,
    meta: { errorToast: false },
    retry: false,
  })
  const columns = useSessionColumns(
    isAdminView,
    expanded,
    toggleSession,
    data?.total_tokens_sum,
    sensitiveVisible,
    data?.range_total_tokens_sum
  )
  const cacheSummary = cacheSummaryQuery.data
  const cacheSummaryRates = cacheSummary?.summary
  const sourceStats = cacheSummary?.source_stats
  const cacheSummaryLabels =
    cacheSummary?.provider === 'mixed'
      ? {
          current: 'Current strategy upper bound',
          adjusted: 'After strategy adjustment',
          adjusted1h: 'After extended retention',
        }
      : skeletonTierLabels(cacheSummary?.provider ?? '')
  const { table } = useDataTable({
    data: data?.items || [],
    columns: columns as ColumnDef<LogSession, unknown>[],
    pagination,
    onPaginationChange,
    sorting,
    onSortingChange: handleSortingChange,
    enableRowSelection: false,
    manualPagination: true,
    manualSorting: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
    columnVisibilityStorageKey: `usage-logs:sessions:${viewAccess}:column-visibility`,
    getRowId: (row) => row.session_id,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns as ColumnDef<LogSession, unknown>[]}
      isLoading={isLoading || (isFetching && !data)}
      isFetching={isFetching}
      emptyTitle={
        sessionsEnabled
          ? t('No Sessions Found')
          : t('Session view is unavailable')
      }
      emptyDescription={
        sessionsEnabled
          ? t('Sessions appear here for requests that carry a session ID.')
          : t('Session view requires a ClickHouse log database')
      }
      skeletonKeyPrefix='usage-log-session-skeleton'
      applyHeaderSize
      hideMobile
      fixedHeight={false}
      className='h-full overflow-y-auto'
      tableClassName='[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_th]:text-[13px]'
      toolbar={
        <div className='flex flex-col gap-2'>
          {props.toolbar}
          {isAdminView &&
            sessionsEnabled &&
            cacheSummary?.enabled !== false && (
              <section
                aria-label={t('Cache diagnosis')}
                aria-live='polite'
                className='flex flex-wrap items-center gap-x-4 gap-y-1 border-b py-2 text-xs'
              >
                <span className='font-semibold'>{t('Summary')}</span>
                {cacheSummaryQuery.isLoading && (
                  <span>{t('Calculating...')}</span>
                )}
                {cacheSummaryQuery.data?.enabled && cacheSummaryRates && (
                  <>
                    <span>
                      {t('Actual hit rate')}{' '}
                      <b>
                        {skeletonRate(
                          cacheSummaryRates.actual,
                          cacheSummaryRates.total
                        )}
                      </b>
                    </span>
                    <span>
                      {t(cacheSummaryLabels.current)}{' '}
                      <b>
                        {skeletonRate(
                          cacheSummaryRates.current,
                          cacheSummaryRates.total
                        )}
                      </b>
                    </span>
                    <span>
                      {t(cacheSummaryLabels.adjusted)}{' '}
                      <b>
                        {skeletonRate(
                          cacheSummaryRates.adjusted,
                          cacheSummaryRates.total
                        )}
                      </b>
                    </span>
                    <span>
                      {t(cacheSummaryLabels.adjusted1h)}{' '}
                      <b>
                        {skeletonRate(
                          cacheSummaryRates.adjusted_1h,
                          cacheSummaryRates.total
                        )}
                      </b>
                    </span>
                    {cacheSummary.provider === 'claude' && (
                      <>
                        <span>
                          {t('After breakpoint advancement (simulated)')}{' '}
                          <b>
                            {skeletonRate(
                              cacheSummaryRates.advanced,
                              cacheSummaryRates.total
                            )}
                          </b>
                        </span>
                        <span>
                          {t('Advancement + 1h TTL (simulated)')}{' '}
                          <b>
                            {skeletonRate(
                              cacheSummaryRates.advanced_1h,
                              cacheSummaryRates.total
                            )}
                          </b>
                        </span>
                      </>
                    )}
                    {cacheSummaryQuery.data.truncated && (
                      <span className='text-warning'>
                        {t('partial sample (scan limit reached)')}
                      </span>
                    )}
                  </>
                )}
                {cacheSummary?.enabled && (
                  <>
                    {sourceStats && (
                      <div className='basis-full overflow-x-auto pt-1'>
                        <table className='min-w-[520px] border-collapse text-xs'>
                          <caption className='sr-only'>
                            {t('Content fallback comparison')}
                          </caption>
                          <thead>
                            <tr className='text-muted-foreground text-left'>
                              <th className='pr-4 font-normal' />
                              <th className='pr-4 font-normal'>
                                {t('Session count')}
                              </th>
                              <th className='pr-4 font-normal'>
                                {t('Requests')}
                              </th>
                              <th className='pr-4 font-normal'>
                                {t('Token count')}
                              </th>
                              <th className='font-normal'>
                                {t('Aggregate hit rate')}
                              </th>
                            </tr>
                          </thead>
                          <tbody>
                            {[
                              {
                                label: t('Claude content fallback'),
                                summary: sourceStats.content_fallback,
                              },
                              {
                                label: t('Non-content fallback'),
                                summary: sourceStats.non_content_fallback,
                              },
                            ].map((item) => (
                              <tr key={item.label} className='border-t'>
                                <th className='py-1 pr-4 text-left font-medium'>
                                  {item.label}
                                </th>
                                <td className='py-1 pr-4'>
                                  {item.summary.session_count}
                                </td>
                                <td className='py-1 pr-4'>
                                  {item.summary.request_count}
                                </td>
                                <td className='py-1 pr-4'>
                                  {item.summary.token_count}
                                </td>
                                <td className='py-1'>
                                  {skeletonRate(
                                    item.summary.cache.cache_read,
                                    item.summary.cache.input_all
                                  )}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    )}
                    <ByteCacheCoverageSummary
                      coverage={cacheSummary.coverage}
                    />
                    <CacheLossBreakdown summary={cacheSummary.loss_summary} />
                  </>
                )}
              </section>
            )}
          <p className='text-muted-foreground text-xs'>
            {t(
              'Sessions are listed when their lifetime overlaps the time range. Only user, token, model, group, channel, customer and session ID filters apply.'
            )}
          </p>
        </div>
      }
      renderRow={(row) => {
        const sessionId = row.original.session_id
        const isExpanded = expanded.has(sessionId)
        return (
          <Fragment key={row.id}>
            <DataTableRow
              row={row}
              className='cursor-pointer transition-colors'
              cellRenderColumns={table.options.columns}
              onClick={(event) => {
                // Buttons in the row (expand, copy) handle their own clicks.
                if ((event.target as HTMLElement).closest('button, a')) return
                toggleSession(sessionId)
              }}
            />
            {isExpanded && (
              <TableRow className='hover:bg-transparent'>
                <TableCell
                  colSpan={row.getVisibleCells().length}
                  className='bg-muted/30 p-0 pl-8'
                >
                  {isAdminView && (
                    <SessionCacheDiagnosis sessionId={sessionId} />
                  )}
                  <SessionRequestsTable sessionId={sessionId} />
                </TableCell>
              </TableRow>
            )}
          </Fragment>
        )
      }}
    />
  )
}
