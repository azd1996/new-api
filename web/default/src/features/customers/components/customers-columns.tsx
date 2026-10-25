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
import { useTranslation } from 'react-i18next'

import { TableId } from '@/components/table-id'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatTimestampToDate } from '@/lib/format'

import { getPrefixDiscountEntries } from '../lib'
import type { Customer } from '../types'
import { DataTableRowActions } from './data-table-row-actions'

export function useCustomersColumns(): ColumnDef<Customer>[] {
  const { t } = useTranslation()
  return [
    {
      accessorKey: 'id',
      header: t('ID'),
      meta: { mobileHidden: true },
      cell: ({ row }) => (
        <TableId value={row.getValue('id') as number} className='w-[60px]' />
      ),
      size: 80,
    },
    {
      accessorKey: 'customer_id',
      header: t('Customer ID'),
      meta: { mobileTitle: true },
      cell: ({ row }) => (
        <span className='font-mono text-sm'>{row.getValue('customer_id')}</span>
      ),
      size: 200,
    },
    {
      accessorKey: 'name',
      header: t('Customer Name'),
      cell: ({ row }) => (
        <span className='font-medium'>{row.getValue('name') || '-'}</span>
      ),
      size: 160,
    },
    {
      id: 'prefix_discounts',
      header: t('Series Discounts'),
      meta: { mobileHidden: true },
      enableSorting: false,
      cell: ({ row }) => {
        const entries = getPrefixDiscountEntries(row.original.prefix_discounts)
        if (entries.length === 0) {
          return <span className='text-muted-foreground text-sm'>-</span>
        }
        const summary = entries
          .map((entry) => `${entry.prefix} ×${entry.discount}`)
          .join('   ')
        return (
          <Tooltip>
            <TooltipTrigger
              render={
                <span className='block max-w-[240px] cursor-help truncate font-mono text-sm' />
              }
            >
              {summary}
            </TooltipTrigger>
            <TooltipContent>
              <div className='space-y-1 text-xs'>
                {entries.map((entry) => (
                  <div key={entry.prefix}>
                    {entry.prefix}: ×{entry.discount}
                  </div>
                ))}
              </div>
            </TooltipContent>
          </Tooltip>
        )
      },
      size: 260,
    },
    {
      accessorKey: 'remark',
      header: t('Remark'),
      meta: { mobileHidden: true },
      enableSorting: false,
      cell: ({ row }) => (
        <span className='text-muted-foreground text-sm'>
          {row.getValue('remark') || '-'}
        </span>
      ),
      size: 200,
    },
    {
      accessorKey: 'created_time',
      header: t('Created'),
      meta: { mobileHidden: true },
      cell: ({ row }) => (
        <div className='min-w-[160px] font-mono text-sm'>
          {formatTimestampToDate(row.getValue('created_time'))}
        </div>
      ),
      size: 180,
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => <DataTableRowActions row={row} />,
      meta: { pinned: 'right' as const },
    },
  ]
}
