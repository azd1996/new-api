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
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { api } from '@/lib/api'

import type { Channel } from '../../types'
import { ChannelsProvider } from '../channels-provider'
import { ChannelsTable } from '../channels-table'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})
const clients: QueryClient[] = []

function channel(name: string): Channel {
  return {
    id: 3,
    type: 1,
    resource_type_name: '',
    key: '',
    status: 1,
    name,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    other: '',
    balance: 0,
    balance_updated_time: 0,
    models: 'gpt-4o',
    group: 'vip',
    used_quota: 0,
    other_info: '',
    remark: '',
    max_input_tokens: 0,
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    settings: '{}',
  }
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  localStorage.clear()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
})

async function renderChannelsPage(
  searchGate: () => Promise<void>,
  options?: {
    initialEntry: string
    initialItems?: Channel[]
    initialTotal?: number
    items: Channel[]
    total: number
  }
) {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/channel/search') {
      await searchGate()
      return {
        data: {
          success: true,
          data: {
            items: options?.items ?? [channel('prod')],
            total: options?.total ?? 1,
            type_counts: {},
          },
        },
      }
    }
    if (url === '/api/channel') {
      return {
        data: {
          success: true,
          data: {
            items: options?.initialItems ?? [
              { ...channel('staging'), id: 9, tag: 'shared' },
            ],
            total: options?.initialTotal ?? 1,
            type_counts: {},
          },
        },
      }
    }
    return { data: { success: true, data: ['vip'] } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const channelsRoute = createRoute({
    getParentRoute: () => auth,
    path: 'channels/',
    component: () => (
      <ChannelsProvider>
        <SectionPageLayout>
          <SectionPageLayout.Content>
            <ChannelsTable />
          </SectionPageLayout.Content>
        </SectionPageLayout>
      </ChannelsProvider>
    ),
  })
  const initialEntry =
    options?.initialEntry ??
    '/channels/?filter=prod&group=%5B%22vip%22%5D&status=%5B%22enabled%22%5D&model=gpt-4o'
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([channelsRoute])]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  })
  await router.load()
  render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Refresh' })).toHaveAttribute(
      'aria-busy',
      'false'
    )
  )
  return get
}

it('refetches the channel list with the current filters and marks Refresh busy while it runs', async () => {
  let gate = Promise.resolve()
  const get = await renderChannelsPage(() => gate)
  const searchCalls = () =>
    get.mock.calls.filter(([url]) => url === '/api/channel/search')
  const refresh = screen.getByRole('button', { name: 'Refresh' })
  expect(searchCalls()).toHaveLength(1)
  expect(refresh).toHaveAttribute('aria-busy', 'false')

  let release!: () => void
  gate = new Promise((resolve) => {
    release = resolve
  })
  refresh.focus()
  await userEvent.setup().keyboard('{Enter}')

  await waitFor(() => expect(searchCalls()).toHaveLength(2))
  expect(searchCalls()[1][1]?.params).toEqual(searchCalls()[0][1]?.params)
  expect(refresh).toHaveAttribute('aria-busy', 'true')
  release()
  await waitFor(() => expect(refresh).toHaveAttribute('aria-busy', 'false'))
})

it('searches individual matches in tag mode and restores tag listing when the keyword is cleared', async () => {
  localStorage.setItem('enable-tag-mode', 'true')
  localStorage.setItem('channels:view-mode', 'table')
  const get = await renderChannelsPage(() => Promise.resolve(), {
    initialEntry: '/channels/?filter=prod&pageSize=10',
    items: [
      { ...channel('prod-east'), id: 1, tag: 'shared' },
      { ...channel('prod-west'), id: 2, tag: 'shared' },
      { ...channel('prod-untagged'), id: 3 },
    ],
    total: 11,
  })
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith('/api/channel/search', {
      params: expect.objectContaining({
        keyword: 'prod',
        tag_mode: false,
        p: 1,
        page_size: 10,
      }),
    })
  )
  expect(await screen.findByText('prod-untagged')).toBeVisible()
  const tagRow = screen.getByRole('row', { name: /Tag：shared/ })
  expect(within(tagRow).getByText('2 channels')).toBeVisible()
  await userEvent.setup().click(within(tagRow).getAllByRole('button')[0])
  expect(await screen.findByText('prod-east')).toBeVisible()
  expect(screen.getByText('prod-west')).toBeVisible()
  expect(screen.queryByText('staging')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Go to next page' })).toBeEnabled()

  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: 'Go to next page' }))
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith('/api/channel/search', {
      params: expect.objectContaining({ tag_mode: false, p: 2, page_size: 10 }),
    })
  )
  await userEvent
    .setup()
    .clear(screen.getByPlaceholderText('Filter by name, ID, or key...'))
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith('/api/channel', {
      params: expect.objectContaining({ tag_mode: true, p: 1 }),
    })
  )
  await waitFor(() =>
    expect(screen.queryByText('prod-untagged')).not.toBeInTheDocument()
  )
  expect(screen.getByRole('row', { name: /Tag：shared/ })).toHaveTextContent(
    '1 channels'
  )
  expect(screen.getByRole('button', { name: 'Go to next page' })).toBeDisabled()
})

it('does not show the previous tag aggregate while a tag-mode search is pending', async () => {
  localStorage.setItem('enable-tag-mode', 'true')
  localStorage.setItem('channels:view-mode', 'table')
  const tagChannels = Array.from({ length: 54 }, (_, index) => ({
    ...channel(`tag-channel-${index + 1}`),
    id: index + 1,
    tag: 'shared',
  }))
  let gate = Promise.resolve()
  const get = await renderChannelsPage(() => gate, {
    initialEntry: '/channels/?pageSize=10',
    initialItems: tagChannels,
    initialTotal: 54,
    items: [
      { ...channel('matching-channel-1'), id: 101 },
      { ...channel('matching-channel-2'), id: 102 },
    ],
    total: 2,
  })

  expect(
    within(screen.getByRole('row', { name: /Tag：shared/ })).getByText(
      '54 channels'
    )
  ).toBeVisible()

  let release!: () => void
  gate = new Promise((resolve) => {
    release = resolve
  })
  await userEvent
    .setup()
    .type(
      screen.getByPlaceholderText('Filter by name, ID, or key...'),
      'matching'
    )

  await waitFor(() =>
    expect(get).toHaveBeenCalledWith('/api/channel/search', {
      params: expect.objectContaining({
        keyword: 'matching',
        tag_mode: false,
      }),
    })
  )
  expect(screen.queryByText('54 channels')).not.toBeInTheDocument()

  release()
  expect(await screen.findByText('matching-channel-1')).toBeVisible()
  expect(screen.getByText('matching-channel-2')).toBeVisible()
  expect(screen.queryByText('54 channels')).not.toBeInTheDocument()
})
