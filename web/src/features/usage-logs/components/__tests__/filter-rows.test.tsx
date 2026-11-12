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
  if (!row) throw new Error(`Filter row not found: ${label}`)
  return row
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
})

it('groups the customer and downstream filters into the last advanced row', async () => {
  await renderAdminFilter()

  const rows = [...document.querySelectorAll('[data-slot="logs-filter-row"]')]
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

it('puts the retry rule keyword filter beside error keyword with an exclude toggle', async () => {
  await renderAdminFilter()

  expect(rowOf('Retry Rule')).toBe(rowOf('Error Keyword'))

  const toggle = screen.getByRole('button', { name: 'Retry Rule: Equals' })
  await userEvent.click(toggle)
  expect(
    screen.getByRole('button', { name: 'Retry Rule: Exclude' })
  ).toHaveAttribute('aria-pressed', 'true')
})

it('keeps Session ID Source beside Session ID in the first advanced row', async () => {
  await renderAdminFilter()

  const sessionIdRow = rowOf('Session ID')
  expect(rowOf('Session ID Source')).toBe(sessionIdRow)

  const fields = [...sessionIdRow.children]
  const sessionIdIndex = fields.findIndex((field) =>
    field.contains(screen.getByPlaceholderText('Session ID'))
  )
  const sourceIndex = fields.findIndex((field) =>
    field.contains(screen.getByRole('combobox', { name: 'Session ID Source' }))
  )
  expect(sourceIndex).toBe(sessionIdIndex + 1)
})

it('keeps common log filter columns adaptive instead of splitting them into twelve narrow tracks', async () => {
  await renderAdminFilter()

  const primaryFilters = screen.getByRole('button', {
    name: 'Model Name: Equals',
  }).parentElement?.parentElement?.parentElement
  expect(primaryFilters?.className).toContain(
    'sm:grid-cols-[repeat(auto-fit,minmax(10rem,1fr))]'
  )
  expect(primaryFilters?.className).not.toContain('lg:grid-cols-12')

  const advancedRows = [
    ...document.querySelectorAll('[data-slot="logs-filter-row"]'),
  ]
  expect(advancedRows.length).toBeGreaterThan(0)
  expect(advancedRows[0].className).toContain(
    'sm:grid-cols-[repeat(auto-fill,minmax(10rem,1fr))]'
  )
  expect(advancedRows[0].className).not.toContain('lg:grid-cols-12')
})

it('gives group, node name and customer ID filters two adaptive grid tracks', async () => {
  await renderAdminFilter()

  let groupField: HTMLElement | null = screen.getByRole('combobox', {
    name: 'Group',
  })
  while (groupField && !groupField.classList.contains('sm:col-span-2')) {
    groupField = groupField.parentElement
  }
  expect(groupField).not.toBeNull()

  const nodeRow = rowOf('Node Name')
  expect(nodeRow.children[0].classList.contains('sm:col-span-2')).toBe(true)

  const customerRow = rowOf('Customer ID')
  expect(customerRow.children[1].classList.contains('sm:col-span-2')).toBe(true)
})
