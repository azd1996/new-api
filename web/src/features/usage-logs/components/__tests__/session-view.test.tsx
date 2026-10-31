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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

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

function mockApi() {
  return vi.spyOn(api, 'get').mockImplementation(async (url: string) => {
    if (url.startsWith('/api/log/self/sessions')) {
      return {
        data: {
          success: true,
          data: { items: [session], total: 1, page: 1, page_size: 20 },
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

async function renderSessions() {
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
      initialEntries: ['/usage-logs/common?view=sessions'],
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
  return screen.findByRole('button', { name: 'Expand session' })
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

it('lists sessions collapsed and loads no session requests before expanding', async () => {
  const get = mockApi()
  const toggle = await renderSessions()

  expect(screen.getByText('sess-abc')).toBeVisible()
  expect(toggle).toHaveAttribute('aria-expanded', 'false')
  expect(
    get.mock.calls.some(([url]) => String(url).startsWith('/api/log/self?'))
  ).toBe(false)
})

it('expands a session in place and loads only its requests', async () => {
  const get = mockApi()
  const toggle = await renderSessions()

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
  })
})
