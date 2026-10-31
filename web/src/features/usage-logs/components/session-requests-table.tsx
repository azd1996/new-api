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
import type { ColumnDef, PaginationState } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePagination,
  DataTableView,
  useDataTable,
} from '@/components/data-table'
import { createServerError } from '@/lib/server-error-message'

import { DEFAULT_LOGS_DATA } from '../constants'
import { useColumnsByCategory } from '../lib/columns'
import { fetchLogsByCategory } from '../lib/utils'
import { useLogsViewScope } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const SESSION_REQUESTS_PAGE_SIZE = 20

interface SessionRequestsTableProps {
  sessionId: string
}

// SessionRequestsTable lists the requests of one session with the regular log
// columns, so each row keeps the usual detail dialog. It reuses the page's
// filters but drops the log type filter: a session covers consume and error
// logs alike.
export function SessionRequestsTable(props: SessionRequestsTableProps) {
  const { t } = useTranslation()
  const { isAdminView, isRootView, viewAccess } = useLogsViewScope()
  const searchParams = route.useSearch()
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: SESSION_REQUESTS_PAGE_SIZE,
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'logs',
      'session-requests',
      props.sessionId,
      viewAccess,
      pagination.pageIndex + 1,
      pagination.pageSize,
      searchParams,
    ],
    queryFn: async () => {
      const result = await fetchLogsByCategory({
        logCategory: 'common',
        isAdmin: isAdminView,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams: {
          ...searchParams,
          type: undefined,
          sessionId: props.sessionId,
        },
        columnFilters: [],
      })
      if (!result?.success) {
        throw createServerError(result, t('Failed to load logs'))
      }
      return result.data || DEFAULT_LOGS_DATA
    },
    placeholderData: (previousData) => previousData,
  })

  const columns = useColumnsByCategory(
    'common',
    isAdminView,
    isRootView,
    false
  ) as ColumnDef<Record<string, unknown>>[]

  const { table } = useDataTable({
    data: (data?.items || []) as Record<string, unknown>[],
    columns,
    pagination,
    onPaginationChange: setPagination,
    enableRowSelection: false,
    manualPagination: true,
    totalCount: data?.total || 0,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })

  return (
    <div className='flex flex-col gap-2 py-2'>
      <DataTableView
        table={table}
        isLoading={isLoading || (isFetching && !data)}
        emptyTitle={t('No Logs Found')}
        skeletonKeyPrefix={`session-${props.sessionId}`}
        applyHeaderSize
        tableClassName='[&_[data-slot=table]]:text-[13px]'
      />
      {(data?.total || 0) > pagination.pageSize && (
        <DataTablePagination table={table} compact />
      )}
    </div>
  )
}
