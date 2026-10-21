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
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'

import type { SchedulePreviewResult } from './types'

interface Props {
  result: SchedulePreviewResult
}

export function PreviewPanel(props: Props) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2 rounded-md border p-3'>
      <div className='grid gap-1'>
        <span className='text-xs font-medium'>{t('Resolved PromQL')}</span>
        <code className='bg-muted block overflow-x-auto rounded p-2 font-mono text-xs'>
          {props.result.resolved_promql || '-'}
        </code>
      </div>
      <StaticDataTable
        tableClassName='min-w-max'
        data={props.result.rows}
        getRowKey={(_row, index) => index}
        emptyClassName='text-muted-foreground py-6'
        emptyContent={t('No rows returned')}
        columns={[
          {
            id: 'label',
            header: t('Label'),
            cellClassName: 'font-medium',
            cell: (row) => row.label || '-',
          },
          {
            id: 'value',
            header: t('Value'),
            cellClassName: 'text-right',
            className: 'text-right',
            cell: (row) => row.value,
          },
        ]}
      />
    </div>
  )
}
