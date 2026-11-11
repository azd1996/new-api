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
import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import { TimingMetricsCell } from '../timing-metrics-cell'

const baseProps = {
  useTimeSec: 9,
  completionTokens: 259,
  frtMs: 7620,
  isStream: true,
}

it('shows gateway and upstream values at the same precision so the gap adds up', () => {
  // use_time is 2s (whole seconds) but the precise latency is 1.30s.
  render(
    <TimingMetricsCell
      {...baseProps}
      useTimeSec={2}
      frtMs={650}
      upstream={{
        sourceLabel: 'AWS',
        ourFirstTokenMs: 650,
        ourTotalMs: 1300,
        upstreamFirstTokenMs: 520,
        upstreamTotalMs: 1150,
        firstTokenGapMs: 130,
        totalGapMs: 150,
      }}
    />
  )

  expect(screen.getByText('0.65s')).toBeVisible()
  expect(screen.getByText('AWS 0.52s')).toBeVisible()
  expect(screen.getByText('+0.13s')).toBeVisible()
  expect(screen.getByText('1.30s')).toBeVisible()
  expect(screen.getByText('AWS 1.15s')).toBeVisible()
  expect(screen.getByText('+0.15s')).toBeVisible()
  expect(screen.queryByText('2.0s')).toBeNull()
})

it('renders only gateway timing when no upstream timing was reported', () => {
  render(<TimingMetricsCell {...baseProps} upstream={null} />)

  expect(screen.getByText('7.6s')).toBeVisible()
  expect(screen.queryByText(/^AWS /)).toBeNull()
})
