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
import type { ColumnDef } from '@tanstack/react-table'
import { ChevronRight } from 'lucide-react'
import { useMemo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { DataTableColumnHeader } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import {
  formatLogQuota,
  formatPercent,
  formatTimestampToDate,
  formatTokens,
  formatUseTime,
} from '@/lib/format'
import { cn } from '@/lib/utils'

import type { LogSession } from '../../types'

// A session cell value that can carry several distinct values (users, tokens,
// models): the representative value plus how many others the session used.
function renderMultiValue(value: string, count: number): ReactNode {
  if (!value) return <span className='text-muted-foreground'>-</span>
  return (
    <span className='flex min-w-0 items-center gap-1'>
      <span className='truncate'>{value}</span>
      {count > 1 && (
        <span className='text-muted-foreground shrink-0 text-xs'>
          +{count - 1}
        </span>
      )}
    </span>
  )
}

export function useSessionColumns(
  isAdmin: boolean,
  expanded: ReadonlySet<string>,
  onToggle: (sessionId: string) => void,
  totalTokensSum?: number,
  sensitiveVisible = true,
  rangeTotalTokensSum?: number
): ColumnDef<LogSession>[] {
  const { t } = useTranslation()

  return useMemo(() => {
    const columns: ColumnDef<LogSession>[] = [
      {
        accessorKey: 'session_id',
        header: t('Session ID'),
        cell: ({ row }) => {
          const session = row.original
          const isExpanded = expanded.has(session.session_id)
          return (
            <div className='flex min-w-0 items-center gap-1'>
              <Button
                variant='ghost'
                size='icon'
                className='size-6 shrink-0'
                aria-expanded={isExpanded}
                aria-label={t(
                  isExpanded ? 'Collapse session' : 'Expand session'
                )}
                onClick={() => onToggle(session.session_id)}
              >
                <ChevronRight
                  aria-hidden='true'
                  className={cn(
                    'size-4 transition-transform',
                    isExpanded && 'rotate-90'
                  )}
                />
              </Button>
              <span
                className='truncate font-mono text-xs'
                title={session.session_id}
              >
                {session.session_id}
              </span>
              <CopyButton
                value={session.session_id}
                className='size-6 shrink-0'
                iconClassName='size-3'
              />
            </div>
          )
        },
        enableHiding: false,
        enableSorting: false,
        size: 260,
      },
      {
        accessorKey: 'last_at',
        header: t('Time'),
        cell: ({ row }) => {
          const session = row.original
          return (
            <div className='flex flex-col gap-0.5 font-mono text-xs tabular-nums'>
              <span>{formatTimestampToDate(session.first_at)}</span>
              <span className='text-muted-foreground'>
                {formatTimestampToDate(session.last_at)}
              </span>
            </div>
          )
        },
        enableSorting: false,
        size: 170,
      },
      {
        id: 'duration',
        header: t('Duration'),
        cell: ({ row }) =>
          formatUseTime(row.original.last_at - row.original.first_at),
        enableSorting: false,
        size: 90,
      },
      {
        accessorKey: 'request_count',
        header: t('Requests'),
        cell: ({ row }) => {
          const session = row.original
          return (
            <div className='flex flex-col gap-0.5'>
              <span className='tabular-nums'>{session.request_count}</span>
              {session.error_count > 0 && (
                <span className='text-xs text-rose-600 tabular-nums dark:text-rose-400'>
                  {t('Errors')}: {session.error_count}
                </span>
              )}
            </div>
          )
        },
        enableSorting: false,
        size: 90,
      },
    ]

    if (isAdmin) {
      columns.push({
        accessorKey: 'username',
        header: t('User'),
        cell: ({ row }) =>
          renderMultiValue(row.original.username, row.original.user_count),
        enableSorting: false,
        size: 120,
      })
    }

    columns.push({
      id: 'customers',
      header: t('Customer ID'),
      accessorFn: (row) =>
        (row.customers ?? [])
          .map((customer) => customer.customer_id)
          .join(', '),
      cell: ({ row }) => {
        const customers = row.original.customers ?? []
        if (customers.length === 0) {
          return <span className='text-muted-foreground'>-</span>
        }
        const customerLabels = customers.map((customer) =>
          sensitiveVisible && customer.name
            ? `${customer.customer_id}（${customer.name}）`
            : customer.customer_id
        )
        return (
          <span
            className='block max-w-60 truncate'
            title={customerLabels.join(', ')}
          >
            {customerLabels.join(', ')}
          </span>
        )
      },
      enableSorting: false,
      size: 200,
    })

    if (sensitiveVisible) {
      columns.push({
        accessorKey: 'token_name',
        header: t('Token'),
        cell: ({ row }) =>
          renderMultiValue(row.original.token_name, row.original.token_count),
        enableSorting: false,
        size: 120,
      })
    }

    columns.push(
      {
        accessorKey: 'model_name',
        header: t('Model'),
        cell: ({ row }) =>
          renderMultiValue(row.original.model_name, row.original.model_count),
        enableSorting: false,
        size: 160,
      },
      {
        id: 'tokens',
        header: t('Input / Output'),
        cell: ({ row }) => (
          <span className='tabular-nums'>
            {formatTokens(row.original.prompt_tokens)} /{' '}
            {formatTokens(row.original.completion_tokens)}
          </span>
        ),
        enableSorting: false,
        size: 120,
      },
      {
        accessorKey: 'total_tokens',
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Total Tokens')} />
        ),
        cell: ({ row }) => {
          const total = row.original.total_tokens
          const share =
            totalTokensSum && totalTokensSum > 0
              ? (total / totalTokensSum) * 100
              : undefined
          return (
            <div className='flex flex-col gap-0.5'>
              <span className='tabular-nums'>{formatTokens(total)}</span>
              {share !== undefined && (
                <span className='text-muted-foreground text-xs tabular-nums'>
                  {t('Share')}: {formatPercent(share)}
                </span>
              )}
            </div>
          )
        },
        size: 120,
      },
      {
        accessorKey: 'range_total_tokens',
        header: ({ column }) => (
          <DataTableColumnHeader
            column={column}
            title={`${t('Total Tokens')} (${t('Time')})`}
          />
        ),
        cell: ({ row }) => {
          const total = row.original.range_total_tokens
          const share =
            rangeTotalTokensSum && rangeTotalTokensSum > 0
              ? (total / rangeTotalTokensSum) * 100
              : undefined
          return (
            <div className='flex flex-col gap-0.5'>
              <span className='tabular-nums'>{formatTokens(total)}</span>
              {share !== undefined && (
                <span className='text-muted-foreground text-xs tabular-nums'>
                  {t('Share')}: {formatPercent(share)}
                </span>
              )}
            </div>
          )
        },
        size: 130,
      },
      {
        accessorKey: 'quota',
        header: t('Cost'),
        cell: ({ row }) => formatLogQuota(row.original.quota),
        enableSorting: false,
        size: 110,
      },
      {
        id: 'cache',
        header: t('Cache Hit Rate'),
        cell: ({ row }) => {
          const cache = row.original.cache
          if (!cache || cache.input_all <= 0) {
            return <span className='text-muted-foreground'>-</span>
          }
          return (
            <div className='flex flex-col gap-0.5'>
              <span className='tabular-nums'>
                {(cache.gross_rate * 100).toFixed(1)}%
              </span>
              <span className='text-muted-foreground text-xs tabular-nums'>
                {t('Hits')}: {cache.hit}/{row.original.consume_count}
              </span>
            </div>
          )
        },
        enableSorting: false,
        size: 110,
      },
      {
        id: 'interval',
        header: t('Request Interval (avg / min / max)'),
        cell: ({ row }) => {
          const session = row.original
          if (session.consume_count < 2) {
            return <span className='text-muted-foreground'>-</span>
          }
          return (
            <span className='text-xs tabular-nums'>
              {formatUseTime(session.avg_interval)} /{' '}
              {formatUseTime(session.min_interval)} /{' '}
              {formatUseTime(session.max_interval)}
            </span>
          )
        },
        enableSorting: false,
        size: 190,
      }
    )

    return columns
  }, [
    t,
    isAdmin,
    expanded,
    onToggle,
    totalTokensSum,
    sensitiveVisible,
    rangeTotalTokensSum,
  ])
}
