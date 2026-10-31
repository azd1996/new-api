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
import { Fragment, useCallback, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { TableCell, TableRow } from '@/components/ui/table'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { getLogSessions } from '../api'
import { buildApiParams } from '../lib/utils'
import type { LogSession } from '../types'
import { useSessionColumns } from './columns/session-columns'
import { SessionRequestsTable } from './session-requests-table'
import { useLogsViewScope } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const DEFAULT_SESSIONS_DATA = { items: [] as LogSession[], total: 0 }

interface UsageLogsSessionsProps {
  toolbar: ReactNode
}

// UsageLogsSessions is the session view of the common logs: one row per
// session_id, expanded in place to list the session's requests.
export function UsageLogsSessions(props: UsageLogsSessionsProps) {
  const { t } = useTranslation()
  const { isAdminView, viewAccess } = useLogsViewScope()
  const searchParams = route.useSearch()
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())

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

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'logs',
      'sessions',
      viewAccess,
      pagination.pageIndex + 1,
      pagination.pageSize,
      searchParams,
    ],
    queryFn: async () => {
      const params = buildApiParams({
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams,
        isAdmin: isAdminView,
      })
      const result = await getLogSessions(params, isAdminView)
      if (!result?.success) {
        throw createServerError(result, t('Failed to load sessions'))
      }
      return result.data || DEFAULT_SESSIONS_DATA
    },
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[2] === viewAccess ? previousData : undefined,
  })

  const columns = useSessionColumns(isAdminView, expanded, toggleSession)
  const { table } = useDataTable({
    data: data?.items || [],
    columns: columns as ColumnDef<LogSession, unknown>[],
    pagination,
    onPaginationChange,
    enableRowSelection: false,
    manualPagination: true,
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
      emptyTitle={t('No Sessions Found')}
      emptyDescription={t(
        'Sessions appear here for requests that carry a session ID.'
      )}
      skeletonKeyPrefix='usage-log-session-skeleton'
      applyHeaderSize
      hideMobile
      tableClassName='[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_th]:text-[13px]'
      toolbar={props.toolbar}
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
