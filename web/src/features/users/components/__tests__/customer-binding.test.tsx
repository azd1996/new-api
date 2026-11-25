/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can use it under the terms of the
GNU Affero General Public License as published by the Free Software
Foundation, either version 3 of the License, or (at your option) any
later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { ROLE } from '@/lib/roles'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { User } from '../../types'
import { UsersMutateDrawer } from '../users-mutate-drawer'
import { UsersProvider } from '../users-provider'

const target: User = {
  id: 2,
  username: 'bound-user',
  display_name: 'Bound user',
  role: ROLE.USER,
  status: 1,
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
  customer_id: 'customer-a',
}

function getControlByLabel(labelText: string): HTMLElement {
  const label = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (candidate) => candidate.textContent?.trim() === labelText
  )
  if (!label) {
    throw new Error(`Expected label "${labelText}"`)
  }

  const control =
    label.control ??
    label
      .closest('[data-slot="form-item"]')
      ?.querySelector<HTMLElement>(
        '[data-slot="form-control"], input, textarea, button[role="combobox"], [role="group"]'
      )
  if (!control) {
    throw new Error(`Expected control for label "${labelText}"`)
  }
  return control
}

function renderDrawer(viewerRole: number) {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'operator', role: viewerRole })
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/authz/catalog') {
      return { data: { success: true, data: { resources: [], roles: [] } } }
    }
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default'] } }
    }
    if (url === '/api/customer/?p=1&page_size=1000') {
      return {
        data: {
          success: true,
          data: {
            items: [
              { customer_id: 'customer-a', name: 'Customer A' },
              { customer_id: 'customer-b', name: '' },
            ],
          },
        },
      }
    }
    return { data: { success: true, data: target } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <UsersProvider>
        <UsersMutateDrawer
          open
          onOpenChange={() => undefined}
          currentRow={target}
        />
      </UsersProvider>
    </QueryClientProvider>
  )
  return get
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

it('admins can change the customer binding of a user', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  renderDrawer(ROLE.ADMIN)

  const customerTrigger = getControlByLabel('Customer') as HTMLButtonElement
  await waitFor(() =>
    expect(customerTrigger.textContent).toContain('customer-a')
  )

  const user = userEvent.setup()
  await user.click(customerTrigger)
  await user.click(await screen.findByRole('option', { name: 'customer-b' }))

  await waitFor(() =>
    expect(customerTrigger.textContent).toContain('customer-b')
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save changes' }))

  await waitFor(() =>
    expect(put).toHaveBeenCalledWith(
      '/api/user/',
      expect.objectContaining({ id: 2, customer_id: 'customer-b' }),
      expect.anything()
    )
  )
})

it('does not offer the customer binding to non-admins', async () => {
  renderDrawer(ROLE.USER)
  await screen.findByDisplayValue('Bound user')
  expect(screen.queryByLabelText('Customer')).toBeNull()
})
