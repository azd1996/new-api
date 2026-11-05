import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import type { SessionCacheAnalysisResponse } from '../../types'
import { SessionCacheDiagnosis } from '../session-cache-diagnosis'

afterEach(cleanup)

function renderDiagnosis(
  provider: string,
  advanced?: number,
  reason = 'breakpoint_stalled'
) {
  const rates = {
    total: 1000,
    actual: 100,
    current: 200,
    adjusted: 300,
    adjusted_1h: 400,
    ...(advanced === undefined ? {} : { advanced, advanced_1h: 900 }),
  }
  const data: SessionCacheAnalysisResponse['data'] = {
    enabled: true,
    summary: rates,
    lanes: [
      {
        lane: 'lane',
        model: provider,
        provider,
        summary: rates,
        turns: [
          {
            request_id: 'r1',
            ts: '2026-11-05T00:00:00Z',
            status: 'ok',
            attempt: 0,
            channel_id: 1,
            key_idx: 0,
            gap_s: 0,
            total_tokens: 1000,
            actual_read: 100,
            expected_current: 200,
            expected_adjusted: 300,
            expected_adjusted_1h: 400,
            ...(advanced === undefined
              ? {}
              : { expected_advanced: advanced, expected_advanced_1h: 900 }),
            reason,
            diverge_idx: -1,
            tail_volatile: false,
            cache_key_changed: false,
            no_cache_key: false,
            total_bytes: 1000,
            segments: null,
            breakpoints: null,
          },
        ],
      },
    ],
  }
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  })
  client.setQueryData(['usage-logs-session-cache', 's1'], data)
  return render(
    <QueryClientProvider client={client}>
      <SessionCacheDiagnosis sessionId='s1' />
    </QueryClientProvider>
  )
}

it('shows Claude advancement simulations separately from tail correction in summary and turns', () => {
  renderDiagnosis('claude', 800)
  expect(
    screen.getByText('After tail breakpoint correction').parentElement
  ).toHaveTextContent('30.0%')
  expect(
    screen.getByText('After breakpoint advancement (simulated)').parentElement
  ).toHaveTextContent('80.0%')
  expect(
    screen.getByText('Advancement + 1h TTL (simulated)').parentElement
  ).toHaveTextContent('90.0%')
  fireEvent.click(screen.getByRole('button', { name: 'Show turns' }))
  const table = screen.getByRole('table')
  expect(within(table).getByRole('cell', { name: '80.0%' })).toBeVisible()
  expect(within(table).getByRole('cell', { name: '90.0%' })).toBeVisible()
  expect(
    within(table).getByText('Breakpoint coverage insufficient / not advanced')
  ).toBeVisible()
})

it('shows missing Claude simulations as unknown in summary and turns', () => {
  renderDiagnosis('claude')
  expect(
    screen.getByText('After breakpoint advancement (simulated)')
  ).toHaveTextContent('—')
  expect(
    screen.getByText('Advancement + 1h TTL (simulated)')
  ).toHaveTextContent('—')
  fireEvent.click(screen.getByRole('button', { name: 'Show turns' }))
  expect(
    within(screen.getByRole('table')).getAllByRole('cell', { name: '—' })
  ).toHaveLength(2)
  expect(
    within(screen.getByRole('table')).queryByRole('cell', { name: '0.0%' })
  ).not.toBeInTheDocument()
})

it('preserves an explicit zero simulation as a measured zero rate', () => {
  renderDiagnosis('claude', 0)
  expect(
    screen.getByText('After breakpoint advancement (simulated)')
  ).toHaveTextContent('0.0%')
  fireEvent.click(screen.getByRole('button', { name: 'Show turns' }))
  expect(
    within(screen.getByRole('table')).getByRole('cell', { name: '0.0%' })
  ).toBeVisible()
})

it('does not show Claude advancement tiers for OpenAI even if new fields are present', () => {
  renderDiagnosis('openai', 800)
  expect(
    screen.queryByText('After breakpoint advancement (simulated)')
  ).not.toBeInTheDocument()
  expect(screen.getByText('With one prompt_cache_key')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Show turns' }))
  expect(
    screen.queryByRole('columnheader', {
      name: 'After breakpoint advancement (simulated)',
    })
  ).not.toBeInTheDocument()
})

it('shows unexplained stable history as its own turn reason', () => {
  renderDiagnosis('claude', 800, 'stable_unexplained')
  fireEvent.click(screen.getByRole('button', { name: 'Show turns' }))
  expect(
    within(screen.getByRole('table')).getByText('Stable history unexplained')
  ).toBeVisible()
})
