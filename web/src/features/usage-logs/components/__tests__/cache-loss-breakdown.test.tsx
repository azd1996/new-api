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
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { CacheLossBreakdown } from '../cache-loss-breakdown'

afterEach(cleanup)

it('keeps analyzed totals without an empty table when all input is cached', () => {
  render(
    <CacheLossBreakdown
      summary={{
        analyzed_turns: 1,
        total_tokens: 1000,
        actual_read: 1000,
        uncached_tokens: 0,
        items: [],
      }}
    />
  )

  expect(
    screen.getByRole('region', { name: 'Cache loss breakdown' })
  ).toBeVisible()
  expect(screen.getByText('Uncached tokens: 0 (0.0%)')).toBeVisible()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
})

it('renders an unknown future reason without dropping its tokens or dividing by zero', () => {
  render(
    <CacheLossBreakdown
      summary={{
        analyzed_turns: 1,
        total_tokens: 0,
        actual_read: 0,
        uncached_tokens: 0,
        items: [{ reason: 'future_reason', tokens: 5, turns: 1 }],
      }}
    />
  )

  expect(screen.getByRole('cell', { name: 'future_reason' })).toBeVisible()
  expect(screen.getByRole('cell', { name: '5' })).toBeVisible()
  expect(screen.getByRole('cell', { name: '—' })).toBeVisible()
})
