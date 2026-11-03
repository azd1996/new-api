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

import { ByteCacheCoverageSummary } from '../cache-coverage-summary'
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

it('shows signed byte coverage change and keeps legacy coverage unavailable', () => {
  const { rerender } = render(
    <ByteCacheCoverageSummary
      coverage={{
        baseline_covered_bytes: 400,
        optimized_covered_bytes: 500,
        delta_bytes: 100,
        total_bytes: 1000,
        comparable_turns: 2,
      }}
    />
  )

  expect(screen.getByText('40.0% → 50.0%')).toBeVisible()
  expect(screen.getByText('+10.0 pp / +100B')).toBeVisible()
  expect(screen.getByRole('region')).toHaveTextContent('Comparable turns: 2')

  rerender(<ByteCacheCoverageSummary />)
  expect(
    screen.getByText('No comparable samples recorded since upgrade')
  ).toBeVisible()
})

it.each([
  {
    baseline: 800,
    optimized: 500,
    delta: -300,
    rates: '80.0% → 50.0%',
    change: '-30.0 pp / -300B',
  },
  {
    baseline: 0,
    optimized: 0,
    delta: 0,
    rates: '0.0% → 0.0%',
    change: '+0.0 pp / 0B',
  },
])('preserves signed coverage with baseline $baseline', (sample) => {
  render(
    <ByteCacheCoverageSummary
      coverage={{
        baseline_covered_bytes: sample.baseline,
        optimized_covered_bytes: sample.optimized,
        delta_bytes: sample.delta,
        total_bytes: 1000,
        comparable_turns: 1,
      }}
    />
  )
  expect(screen.getByText(sample.rates)).toBeVisible()
  expect(screen.getByText(sample.change)).toBeVisible()
})

it.each([
  { comparable_turns: 0 },
  { total_bytes: 0 },
  { delta_bytes: Number.NaN },
  { baseline_covered_bytes: Number.POSITIVE_INFINITY },
])(
  'shows missing samples instead of invalid percentages for %o',
  (override) => {
    render(
      <ByteCacheCoverageSummary
        coverage={{
          baseline_covered_bytes: 400,
          optimized_covered_bytes: 500,
          delta_bytes: 100,
          total_bytes: 1000,
          comparable_turns: 1,
          ...override,
        }}
      />
    )
    expect(
      screen.getByText('No comparable samples recorded since upgrade')
    ).toBeVisible()
    expect(screen.getByRole('region')).not.toHaveTextContent(/NaN|Infinity|%/)
  }
)
