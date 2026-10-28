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
import type { ColumnDef } from '@tanstack/react-table'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { cn } from '@/lib/utils'

import {
  DEFAULT_LOGS_DATA,
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
} from '../constants'
import { useColumnsByCategory } from '../lib/columns'
import { parseLogOther } from '../lib/format'
import { fetchLogsByCategory } from '../lib/utils'
import { getSessionLogSearch, startSessionLogSearch } from '../api'
import type { LogCategory } from '../types'
import { CommonLogsFilterBar } from './common-logs-filter-bar'
import { TaskLogsFilterBar } from './task-logs-filter-bar'
import { UsageLogsMobileList } from './usage-logs-mobile-card'
import { useLogsViewScope } from './usage-logs-provider'
import { UsageLogsStatistics } from './usage-logs-statistics'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

// Warning tint for logs where a quota conversion saturated (admin-only marker).
// Takes precedence over the per-type tint since it flags a billing anomaly.
const quotaSaturationRowTint = 'bg-amber-50/60 dark:bg-amber-950/25'

function getColumnVisibilityStorageKey(
  logCategory: LogCategory,
  isAdmin: boolean
): string {
  return `usage-logs:${logCategory}:${isAdmin ? 'admin' : 'user'}:column-visibility`
}

function deserializeLogTypeFilter(value: unknown): unknown[] {
  let values: unknown[]
  if (Array.isArray(value)) {
    values = value
  } else if (value) {
    values = [value]
  } else {
    values = []
  }
  return values.filter((item) => String(item) !== LOG_TYPE_ALL_VALUE)
}

interface UsageLogsTableProps {
  logCategory: LogCategory
  viewMode?: 'list' | 'stats'
}

export function UsageLogsTable({
  logCategory,
  viewMode = 'list',
}: UsageLogsTableProps) {
  const { t } = useTranslation()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()

  // Session-id async search mode (admin common logs only). When a session id is
  // present the table is driven by the chunked search engine instead of the
  // normal paged list query.
  const sessionId =
    logCategory === 'common' && isAdmin
      ? String(searchParams.sessionId ?? '').trim()
      : ''
  const sessionMode = viewMode === 'list' && sessionId !== ''
  const sessionUsername = String(searchParams.username ?? '')
  const sessionStartSec = searchParams.startTime
    ? Math.floor(Number(searchParams.startTime) / 1000)
    : 0
  const sessionEndSec = searchParams.endTime
    ? Math.floor(Number(searchParams.endTime) / 1000)
    : 0

  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 20 : 30 },
    globalFilter: { enabled: false },
    columnFilters: [
      {
        columnId: 'created_at',
        searchKey: 'type',
        type: 'array' as const,
        deserialize: deserializeLogTypeFilter,
      },
      { columnId: 'model_name', searchKey: 'model', type: 'string' as const },
      { columnId: 'token_name', searchKey: 'token', type: 'string' as const },
      { columnId: 'group', searchKey: 'group', type: 'string' as const },
      ...(isAdmin
        ? [
            {
              columnId: 'channel',
              searchKey: 'channel',
              type: 'string' as const,
            },
            {
              columnId: 'username',
              searchKey: 'username',
              type: 'string' as const,
            },
          ]
        : []),
    ],
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'logs',
      logCategory,
      isAdmin,
      pagination.pageIndex + 1,
      pagination.pageSize,
      columnFilters,
      searchParams,
      t,
    ],
    enabled: !sessionMode && viewMode === 'list',
    queryFn: async () => {
      const result = await fetchLogsByCategory({
        logCategory,
        isAdmin,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams,
        columnFilters,
      })

      if (!result?.success) {
        toast.error(result?.message || t('Failed to load logs'))
        return DEFAULT_LOGS_DATA
      }

      return result.data || DEFAULT_LOGS_DATA
    },
    placeholderData: (previousData, previousQuery) => {
      if (previousQuery?.queryKey[1] === logCategory) {
        return previousData
      }
      return undefined
    },
  })

  const sessionQuery = useQuery({
    queryKey: [
      'session-logs',
      sessionId,
      sessionUsername,
      sessionStartSec,
      sessionEndSec,
      pagination.pageIndex,
      pagination.pageSize,
    ],
    enabled: sessionMode && viewMode === 'list',
    queryFn: async () => {
      const res = await getSessionLogSearch({
        session_id: sessionId,
        username: sessionUsername,
        start_timestamp: sessionStartSec,
        end_timestamp: sessionEndSec,
        page: pagination.pageIndex,
        page_size: pagination.pageSize,
      })
      if (!res?.success) {
        toast.error(res?.message || t('Failed to load logs'))
        return {
          found: false,
          items: [],
          total: 0,
          status: { state: 'error' as const },
          page: pagination.pageIndex,
          page_size: pagination.pageSize,
        }
      }
      return (
        res.data ?? {
          found: false,
          items: [],
          total: 0,
          status: { state: '' as const },
          page: pagination.pageIndex,
          page_size: pagination.pageSize,
        }
      )
    },
    refetchInterval: (query) =>
      query.state.data?.status?.state === 'running' ? 1500 : false,
    placeholderData: (previousData) => previousData,
  })

  const sessionData = sessionMode ? sessionQuery.data : undefined
  const sessionFound = sessionData?.found
  const refetchSession = sessionQuery.refetch

  // On direct load / reload the job may not exist yet; kick it off once when a
  // poll reports it is not found, then let the poll pick up progress.
  useEffect(() => {
    if (!sessionMode || sessionFound !== false) return
    void startSessionLogSearch({
      session_id: sessionId,
      username: sessionUsername,
      start_timestamp: sessionStartSec,
      end_timestamp: sessionEndSec,
    })
      .then(() => {
        void refetchSession()
      })
      .catch(() => {})
  }, [
    sessionMode,
    sessionId,
    sessionUsername,
    sessionStartSec,
    sessionEndSec,
    sessionFound,
    refetchSession,
  ])

  const logs = (sessionMode ? sessionData?.items : data?.items) || []
  const activeTotal = (sessionMode ? sessionData?.total : data?.total) || 0
  const columns = useColumnsByCategory(logCategory, isAdmin)
  const isLoadingData = sessionMode
    ? sessionQuery.isLoading || (sessionQuery.isFetching && !sessionData)
    : isLoading || (isFetching && !data)
  const isFetchingActive = sessionMode ? sessionQuery.isFetching : isFetching

  const { table } = useDataTable({
    data: logs as Record<string, unknown>[],
    columns: columns as ColumnDef<Record<string, unknown>>[],
    columnFilters,
    columnVisibilityStorageKey: getColumnVisibilityStorageKey(
      logCategory,
      isAdmin
    ),
    pagination,
    enableRowSelection: false,
    onPaginationChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: activeTotal,
    ensurePageInRange,
  })

  const isCommon = logCategory === 'common'
  const commonToolbar = (
    <CommonLogsFilterBar
      table={table}
      showStats={viewMode !== 'stats'}
    />
  )

  if (isCommon && viewMode === 'stats') {
    return (
      <div className='flex h-full min-h-0 flex-col gap-4'>
        {commonToolbar}
        <UsageLogsStatistics />
      </div>
    )
  }

  return (
    <DataTablePage
      table={table}
      columns={columns as ColumnDef<Record<string, unknown>>[]}
      isLoading={isLoadingData}
      isFetching={isFetchingActive}
      emptyTitle={t('No Logs Found')}
      emptyDescription={t(
        'No usage logs available. Logs will appear here once API calls are made.'
      )}
      skeletonKeyPrefix='usage-log-skeleton'
      applyHeaderSize
      tableClassName={cn(
        '[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
      )}
      mobile={
        <UsageLogsMobileList
          table={table}
          isLoading={isLoadingData}
          logCategory={logCategory}
        />
      }
      toolbar={
        isCommon ? commonToolbar : (
          <TaskLogsFilterBar table={table} logCategory={logCategory} />
        )
      }
      renderRow={(row) => {
        const logType = (row.original as Record<string, unknown>).type as
          | number
          | undefined
        let tintClass =
          isCommon && logType != null ? (logTypeRowTint[logType] ?? '') : ''
        if (isCommon && isAdmin) {
          const other = parseLogOther(
            ((row.original as Record<string, unknown>).other as string) ?? ''
          )
          if (other?.admin_info?.quota_saturation) {
            tintClass = quotaSaturationRowTint
          }
        }

        return (
          <DataTableRow
            key={row.id}
            row={row}
            className={cn('transition-colors', tintClass)}
            getColumnClassName={() => (isCommon ? 'py-2' : 'py-3.5')}
          />
        )
      }}
    />
  )
}
