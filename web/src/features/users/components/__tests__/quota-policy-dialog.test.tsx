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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { getUserQuotaPolicy, saveUserQuotaPolicy } from '../../api'
import { UserQuotaPolicyDialog } from '../user-quota-policy-dialog'

vi.mock('../../api', () => ({
  getUserQuotaPolicy: vi.fn(),
  saveUserQuotaPolicy: vi.fn(),
}))

const clients: QueryClient[] = []
const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

function renderDialog(props: { onOpenChange?: (open: boolean) => void } = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={client}>
        <UserQuotaPolicyDialog
          open={true}
          onOpenChange={props.onOpenChange ?? (() => {})}
          userId={7}
          currentQuota={500}
          onSuccess={() => {}}
        />
      </QueryClientProvider>
    </I18nextProvider>
  )
}

beforeEach(() => {
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})
afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.clearAllMocks()
})

it('saves a monthly refresh policy with the entered amount', async () => {
  vi.mocked(getUserQuotaPolicy).mockResolvedValue({
    success: true,
    message: '',
    data: { policy: null },
  })
  vi.mocked(saveUserQuotaPolicy).mockResolvedValue({
    success: true,
    message: '',
    data: {
      policy: {
        id: 1,
        user_id: 7,
        mode: 'monthly_refresh',
        amount: 1000,
        enabled: true,
        last_granted_period: '',
        updated_by: 1,
        created_at: 0,
        updated_at: 0,
      },
    },
  })

  renderDialog()
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole('button', { name: 'Monthly refresh' })
  )
  await user.type(screen.getByLabelText('Amount (USD)'), '2')
  await user.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() => {
    expect(saveUserQuotaPolicy).toHaveBeenCalledWith({
      user_id: 7,
      mode: 'monthly_refresh',
      amount: expect.any(Number),
    })
  })
  expect(
    vi.mocked(saveUserQuotaPolicy).mock.calls[0][0].amount
  ).toBeGreaterThan(0)
})

it('clears the stored policy when Off is selected', async () => {
  vi.mocked(getUserQuotaPolicy).mockResolvedValue({
    success: true,
    message: '',
    data: {
      policy: {
        id: 1,
        user_id: 7,
        mode: 'monthly_refresh',
        amount: 1000,
        enabled: true,
        last_granted_period: '2026-11',
        updated_by: 1,
        created_at: 0,
        updated_at: 0,
      },
    },
  })
  vi.mocked(saveUserQuotaPolicy).mockResolvedValue({
    success: true,
    message: '',
    data: { policy: undefined as never },
  })

  renderDialog()
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'Off' }))
  await user.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() => {
    expect(saveUserQuotaPolicy).toHaveBeenCalledWith({
      user_id: 7,
      mode: '',
      amount: 0,
    })
  })
})

it('requires an amount before saving a monthly refresh policy', async () => {
  vi.mocked(getUserQuotaPolicy).mockResolvedValue({
    success: true,
    message: '',
    data: { policy: null },
  })

  renderDialog()
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole('button', { name: 'Monthly refresh' })
  )
  await user.click(screen.getByRole('button', { name: 'Save' }))

  expect(saveUserQuotaPolicy).not.toHaveBeenCalled()
})
