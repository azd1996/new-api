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
import { downloadCsv, toCsvLine } from '@/features/usage-logs/lib/export'
import dayjs from '@/lib/dayjs'

import type { ErrorStatItem, ErrorStatsGroupBy } from '../types'

type Translate = (key: string) => string

function formatSeconds(seconds: number): string {
  if (!seconds) return '-'
  return dayjs(seconds * 1000).format('YYYY-MM-DD HH:mm:ss')
}

function formatBreakdown(
  items: Array<{
    count: number
    model?: string
    channel_name?: string
    channel_id?: number
    customer_id?: string
    customer_name?: string
  }>
): string {
  return items
    .map((item) => {
      if (item.model !== undefined) return `${item.model} ×${item.count}`
      if (item.channel_id !== undefined) {
        const name = item.channel_name || `#${item.channel_id}`
        return `${name} ×${item.count}`
      }
      const customer = item.customer_name
        ? `${item.customer_id}(${item.customer_name})`
        : item.customer_id
      return `${customer} ×${item.count}`
    })
    .join('; ')
}

function getGroupHeader(viewMode: ErrorStatsGroupBy, t: Translate): string {
  return viewMode === 'attribution'
    ? t('Error Attribution')
    : t('Detailed Error Type')
}

export function getErrorStatsCsvHeader(
  viewMode: ErrorStatsGroupBy,
  t: Translate
): string[] {
  const common = [
    t('Occurrences'),
    t('First Seen'),
    t('Last Seen'),
  ]
  const breakdown = [t('Models'), t('Channels'), t('Customers')]
  if (viewMode === 'signature') {
    return [
      t('Detailed Error Type'),
      t('Error Attribution'),
      t('Explanation'),
      t('Detailed Error'),
      ...common,
      t('Status Code'),
      ...breakdown,
    ]
  }
  return [getGroupHeader(viewMode, t), ...common, ...breakdown]
}

export function getErrorStatsCsvRow(
  item: ErrorStatItem,
  viewMode: ErrorStatsGroupBy
): string[] {
  const common = [
    String(item.count),
    formatSeconds(item.first_seen),
    formatSeconds(item.last_seen),
  ]
  const breakdown = [
    formatBreakdown(item.models),
    formatBreakdown(item.channels),
    formatBreakdown(item.customers),
  ]
  if (viewMode === 'signature') {
    return [
      item.short_name,
      item.attribution,
      item.note,
      item.signature,
      ...common,
      String(item.status_code || ''),
      ...breakdown,
    ]
  }
  return [item.group_label, ...common, ...breakdown]
}

export function buildErrorStatsCsvLines(
  items: ErrorStatItem[],
  viewMode: ErrorStatsGroupBy,
  t: Translate
): string[] {
  return [
    toCsvLine(getErrorStatsCsvHeader(viewMode, t)),
    ...items.map((item) => toCsvLine(getErrorStatsCsvRow(item, viewMode))),
  ]
}

export function downloadErrorStatsCsv(
  items: ErrorStatItem[],
  viewMode: ErrorStatsGroupBy,
  startMs: number,
  endMs: number,
  t: Translate
): void {
  const filename = `detailed-error-stats-${viewMode}-${dayjs(startMs).format(
    'YYYYMMDD'
  )}-${dayjs(endMs).format('YYYYMMDD')}.csv`
  downloadCsv(filename, buildErrorStatsCsvLines(items, viewMode, t))
}
