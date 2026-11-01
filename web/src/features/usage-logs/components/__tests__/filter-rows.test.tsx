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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { CommonLogsFilterBar } from '../common-logs-filter-bar'
import { UsageLogsProvider } from '../usage-logs-provider'

function FilterFixture() {
  const table = useReactTable({
    data: [],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <UsageLogsProvider>
      <CommonLogsFilterBar table={table} />
    </UsageLogsProvider>
  )
}

const LIST_ENDPOINTS = new Set([
  '/api/channel/models_enabled',
  '/api/log/nodes',
  '/api/group/',
])

async function renderAdminFilter() {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'admin', role: ROLE.ADMIN })
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: LIST_ENDPOINTS.has(String(url)) ? [] : { quota: 0, rpm: 0, tpm: 0 },
    },
  }))
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const logs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: FilterFixture,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([logs])]),
    history: createMemoryHistory({ initialEntries: ['/usage-logs/common'] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await userEvent.click(await screen.findByRole('button', { name: /Expand/ }))
}

// rowOf returns the advanced-filter row holding the field labelled `label`
// (input placeholder, visible text or accessible name).
function rowOf(label: string) {
  const field =
    screen.queryByPlaceholderText(label) ??
    screen.queryByRole('combobox', { name: label }) ??
    screen.getByText(label)
  const row = field.closest('[data-slot="logs-filter-row"]')
  expect(row, label).not.toBeNull()
  return row
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
})

it('groups the customer and downstream filters into the last advanced row', async () => {
  await renderAdminFilter()

  const rows = [
    ...document.querySelectorAll('[data-slot="logs-filter-row"]'),
  ]
  const customerRow = rowOf('Customer Name')
  for (const label of [
    'Customer ID',
    'Downstream Request ID',
    'Downstream Trace Tag',
  ]) {
    expect(rowOf(label)).toBe(customerRow)
  }
  expect(rows.at(-1)).toBe(customerRow)
})

it('puts node, retry, status code and error keyword on their own row', async () => {
  await renderAdminFilter()

  const nodeRow = rowOf('Node Name')
  for (const label of ['Retry triggered', 'Status Code', 'Error Keyword']) {
    expect(rowOf(label)).toBe(nodeRow)
  }
  expect(nodeRow).not.toBe(rowOf('Session ID'))
  expect(nodeRow).not.toBe(rowOf('Customer Name'))
})
