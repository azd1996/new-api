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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import {
  act,
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import type {
  SessionCacheAnalysisResponse,
  SkeletonLossSummary,
} from '../../types'
import { UsageLogsProvider } from '../usage-logs-provider'
import { UsageLogsSessions } from '../usage-logs-sessions'

const session = {
  session_id: 'sess-abc',
  request_count: 3,
  consume_count: 2,
  error_count: 1,
  first_at: 1_761_900_000,
  last_at: 1_761_900_090,
  quota: 1000,
  prompt_tokens: 200,
  completion_tokens: 30,
  username: 'alice',
  user_count: 1,
  token_name: 'key-a',
  token_count: 1,
  model_name: 'claude-x',
  model_count: 1,
  customers: [
    { customer_id: 'cust-1', name: 'Acme' },
    { customer_id: 'cust-2' },
  ],
  avg_interval: 90,
  min_interval: 90,
  max_interval: 90,
  cache: {
    gross_rate: 0.5,
    eligible_rate: 0.5,
    cache_read: 100,
    input_all: 200,
    input_eligible: 200,
    hit: 1,
    cold_write: 1,
    too_small: 0,
    other_miss: 0,
  },
}

const lossSummary: SkeletonLossSummary = {
  analyzed_turns: 2,
  total_tokens: 1000,
  actual_read: 400,
  uncached_tokens: 600,
  items: [
    { reason: 'new_content', tokens: 150, turns: 1 },
    { reason: 'prefix_changed', tokens: 450, turns: 2 },
  ],
}

function mockApi(
  enabled = true,
  summaryDelay?: Promise<void>,
  options?: {
    provider?: string
    lossSummary?: SkeletonLossSummary
    detail?: SessionCacheAnalysisResponse['data']
  }
) {
  return vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url.startsWith('/api/log/cache-skeleton/session?')) {
      return { data: { success: true, data: options?.detail ?? {} } }
    }
    if (url.startsWith('/api/log/cache-skeleton/sessions/analysis?')) {
      await summaryDelay
      return {
        data: {
          success: true,
          data: {
            enabled: true,
            provider: options?.provider ?? 'openai',
            loss_summary: options?.lossSummary,
            summary: {
              total: 1000,
              actual: 750,
              current: 763,
              adjusted: 763,
              adjusted_1h: 763,
            },
            truncated: false,
          },
        },
      }
    }
    if (
      url.startsWith('/api/log/self/sessions') ||
      url.startsWith('/api/log/sessions?')
    ) {
      return {
        data: {
          success: true,
          data: {
            enabled,
            items: enabled ? [session] : [],
            total: enabled ? 1 : 0,
            page: 1,
            page_size: 20,
          },
        },
      }
    }
    if (url.startsWith('/api/log/self?')) {
      return {
        data: {
          success: true,
          data: {
            items: [
              {
                id: 7,
                type: 2,
                created_at: 1_761_900_090,
                model_name: 'claude-x',
                token_name: 'key-a',
                request_id: 'req-in-session',
                session_id: 'sess-abc',
                other: '',
              },
            ],
            total: 1,
          },
        },
      }
    }
    return { data: { success: true, data: {} } }
  })
}

async function renderSessions(search = '') {
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: () => (
      <UsageLogsProvider>
        <UsageLogsSessions toolbar={null} />
      </UsageLogsProvider>
    ),
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({
      initialEntries: [`/usage-logs/common?view=sessions${search}`],
    }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
})

it('lists sessions collapsed and loads no session requests before expanding', async () => {
  let finishSummary!: () => void
  const summaryDelay = new Promise<void>((resolve) => {
    finishSummary = resolve
  })
  const get = mockApi(true, summaryDelay)
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'admin', role: ROLE.ADMIN })
  await renderSessions()
  const toggle = await screen.findByRole('button', { name: 'Expand session' })

  expect(screen.getByText('sess-abc')).toBeVisible()
  expect(toggle).toHaveAttribute('aria-expanded', 'false')
  expect(screen.getByText('Calculating...')).toBeVisible()
  expect(screen.getByText('Requests')).toBeVisible()
  expect(
    screen.getByRole('table').closest('[class*="space-y-2.5"]')
  ).not.toBeNull()
  await act(async () => finishSummary())
  expect(await screen.findByText('Actual hit rate')).toBeVisible()
  expect(screen.getByText('75.0%')).toBeVisible()
  expect(screen.getByText('Prefix-match ceiling')).toBeVisible()
  expect(screen.getByText('With one prompt_cache_key')).toBeVisible()
  expect(screen.getByText('One key + 24h retention')).toBeVisible()
  expect(
    screen.queryByRole('region', { name: 'Cache loss breakdown' })
  ).not.toBeInTheDocument()
  expect(
    get.mock.calls.some(([url]) => String(url).startsWith('/api/log/self?'))
  ).toBe(false)
  expect(screen.getByText('cust-1（Acme）, cust-2')).toBeVisible()
})

it('shows the Claude-only loss denominator in a mixed-provider filtered summary', async () => {
  const get = mockApi(true, undefined, { provider: 'mixed', lossSummary })
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'admin', role: ROLE.ADMIN })
  await renderSessions('&model_name=claude-x&channel=5')

  const breakdown = await screen.findByRole('region', {
    name: 'Cache loss breakdown',
  })
  expect(
    within(breakdown).getByText('Uncached tokens: 600 (60.0%)')
  ).toBeVisible()
  const rows = within(breakdown).getAllByRole('row')
  expect(rows[1]).toHaveTextContent('Prefix or message history changed')
  expect(
    within(rows[1])
      .getAllByRole('cell')
      .map((cell) => cell.textContent)
  ).toEqual(['Prefix or message history changed', '2', '450', '75.0%'])
  expect(rows[2]).toHaveTextContent('New content / residual')
  expect(screen.getByText('Current strategy upper bound')).toBeVisible()
  const call = get.mock.calls.find(([url]) =>
    String(url).startsWith('/api/log/cache-skeleton/sessions/analysis?')
  )
  expect(String(call?.[0])).toContain('channel=5')
  expect(lossSummary.items[0].reason).toBe('new_content')
})

it('hides losses in a mixed range with no analyzable Claude attempts while keeping rates', async () => {
  mockApi(true, undefined, {
    provider: 'mixed',
    lossSummary: { ...lossSummary, analyzed_turns: 0, items: [] },
  })
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'admin', role: ROLE.ADMIN })
  await renderSessions()

  expect(await screen.findByText('Actual hit rate')).toBeVisible()
  expect(
    screen.queryByRole('region', { name: 'Cache loss breakdown' })
  ).not.toBeInTheDocument()
})

it.each([true, false])(
  'renders session detail with optional losses present=%s and preserves all four rates',
  async (withLosses) => {
    const rates = {
      total: 1000,
      actual: 400,
      current: 600,
      adjusted: 800,
      adjusted_1h: 900,
    }
    mockApi(true, undefined, {
      detail: {
        enabled: true,
        summary: rates,
        loss_summary: withLosses ? lossSummary : undefined,
        lanes: [
          {
            lane: 'claude-lane',
            provider: 'claude',
            model: 'claude-x',
            turns: [],
            summary: rates,
          },
        ],
      },
    })
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'admin', role: ROLE.ADMIN })
    await renderSessions()
    await userEvent.click(
      await screen.findByRole('button', { name: 'Expand session' })
    )

    const showTurns = await screen.findByRole('button', { name: 'Show turns' })
    const diagnosis = showTurns.closest('section')
    if (!diagnosis) throw new Error('Cache diagnosis section not found')
    for (const rate of ['40.0%', '60.0%', '80.0%', '90.0%']) {
      expect(within(diagnosis).getByText(rate)).toBeVisible()
    }
    expect(
      within(diagnosis).queryByRole('region', {
        name: 'Cache loss breakdown',
      }) !== null
    ).toBe(withLosses)
  }
)

it('expands a session in place and loads all its requests regardless of the time range', async () => {
  const get = mockApi()
  await renderSessions('&startTime=1761900000000&endTime=1761903600000')
  const toggle = await screen.findByRole('button', { name: 'Expand session' })

  await userEvent.click(toggle)

  expect(
    await screen.findByRole('button', { name: 'Collapse session' })
  ).toHaveAttribute('aria-expanded', 'true')
  await waitFor(() => {
    const requestCall = get.mock.calls
      .map(([url]) => String(url))
      .find((url) => url.startsWith('/api/log/self?'))
    expect(requestCall).toBeDefined()
    const params = new URLSearchParams(String(requestCall).split('?')[1])
    expect(params.get('session_id')).toBe('sess-abc')
    expect(params.has('type')).toBe(false)
    expect(params.has('start_timestamp')).toBe(false)
    expect(params.has('end_timestamp')).toBe(false)
  })
})

it('explains the view is unavailable when the deployment keeps no session table', async () => {
  mockApi(false)
  await renderSessions()

  expect(
    await screen.findByText('Session view requires a ClickHouse log database')
  ).toBeVisible()
  expect(
    screen.queryByRole('button', { name: 'Expand session' })
  ).not.toBeInTheDocument()
})

it('lets a root user start a backfill for the entered number of days', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'root', role: ROLE.SUPER_ADMIN })
  vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url === '/api/log/sessions/backfill') {
      return {
        data: {
          success: true,
          data: {
            running: false,
            cutoff: 0,
            days: 0,
            done_days: [],
            skipped_days: [],
          },
        },
      }
    }
    if (url.startsWith('/api/log/sessions?')) {
      return {
        data: {
          success: true,
          data: {
            enabled: true,
            items: [session],
            total: 1,
            page: 1,
            page_size: 20,
          },
        },
      }
    }
    return { data: { success: true, data: {} } }
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        running: true,
        cutoff: 1,
        days: 30,
        done_days: [],
        skipped_days: [],
      },
    },
  })
  await renderSessions()

  await userEvent.click(
    await screen.findByRole('button', { name: 'Backfill session history' })
  )
  const daysInput = await screen.findByLabelText('Days to backfill (1-366)')
  await userEvent.clear(daysInput)
  await userEvent.type(daysInput, '30')
  await userEvent.click(screen.getByRole('button', { name: 'Start' }))

  await waitFor(() =>
    expect(post).toHaveBeenCalledWith('/api/log/sessions/backfill?days=30')
  )
  expect(await screen.findByRole('status')).toHaveTextContent(
    'Backfilling: 0 / 30 days done'
  )
})
