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
import { describe, expect, test } from 'vitest'

import type { ErrorStatItem } from '../../types'
import {
  buildErrorStatsCsvLines,
  getErrorStatsCsvHeader,
  getErrorStatsCsvRow,
} from '../export'

const t = (key: string) => key
const firstSeen = new Date(2026, 9, 12, 8, 30, 5).getTime() / 1000
const lastSeen = new Date(2026, 9, 12, 9, 30, 5).getTime() / 1000

const item: ErrorStatItem = {
  group_key: 'hash-1',
  group_label: 'Timeout',
  signature: 'upstream timeout',
  signature_hash: 'hash-1',
  short_name: 'Timeout',
  attribution: 'Upstream',
  note: 'Retryable upstream failure',
  count: 3,
  first_seen: firstSeen,
  last_seen: lastSeen,
  status_code: 504,
  models: [{ model: 'gpt-4o', count: 2 }],
  channels: [{ channel_id: 7, channel_name: 'primary', count: 3 }],
  customers: [{ customer_id: 'c-1', customer_name: 'Acme', count: 3 }],
}

describe('detailed error CSV export', () => {
  test('signature mode exports the same fields as the detailed error table', () => {
    expect(getErrorStatsCsvHeader('signature', t)).toEqual([
      'Detailed Error Type',
      'Error Attribution',
      'Explanation',
      'Detailed Error',
      'Occurrences',
      'First Seen',
      'Last Seen',
      'Status Code',
      'Models',
      'Channels',
      'Customers',
    ])
    expect(getErrorStatsCsvRow(item, 'signature')).toEqual([
      'Timeout',
      'Upstream',
      'Retryable upstream failure',
      'upstream timeout',
      '3',
      '2026-10-12 08:30:05',
      '2026-10-12 09:30:05',
      '504',
      'gpt-4o ×2',
      'primary ×3',
      'c-1(Acme) ×3',
    ])
  })

  test('grouped mode exports the selected group label and no raw signature', () => {
    const lines = buildErrorStatsCsvLines([item], 'short_name', t)

    expect(lines).toEqual([
      '"Detailed Error Type","Occurrences","First Seen","Last Seen","Models","Channels","Customers"\r\n',
      '"Timeout","3","2026-10-12 08:30:05","2026-10-12 09:30:05","gpt-4o ×2","primary ×3","c-1(Acme) ×3"\r\n',
    ])
    expect(lines.join('')).not.toContain('upstream timeout')
  })
})
