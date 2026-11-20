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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { BalanceAlertSettingsSection } from '../balance-alert-settings-section'

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <BalanceAlertSettingsSection
          defaultValues={{
            'balance_alert_setting.enabled': true,
            'balance_alert_setting.rules': [
              {
                id: 'rule-1',
                enabled: true,
                group: 'vip',
                threshold_amount: 20,
                max_users: 10,
                alert_toid: [123, 456],
                alert_url: 'https://example.com/hook',
                link: '',
                at_user_ids: [],
              },
            ],
          }}
        />
      </SettingsPageProvider>
    </>
  )
}

const putMock = vi.fn()

beforeEach(() => {
  putMock.mockReset()
  putMock.mockResolvedValue({ data: { success: true } })
  vi.spyOn(api, 'put').mockImplementation(putMock)
})

test('shows the configured rule fields', async () => {
  render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <Fixture />
    </QueryClientProvider>
  )

  expect(await screen.findByDisplayValue('rule-1')).toBeTruthy()
  expect(screen.getByDisplayValue('vip')).toBeTruthy()
  expect(screen.getByDisplayValue('123, 456')).toBeTruthy()
  expect(
    screen.getByRole('spinbutton', { name: 'Balance threshold' })
  ).toBeTruthy()
})

test('saving writes the toggle and the serialized rules', async () => {
  const user = userEvent.setup()
  render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <Fixture />
    </QueryClientProvider>
  )

  const threshold = await screen.findByRole('spinbutton', {
    name: 'Balance threshold',
  })
  await user.clear(threshold)
  await user.type(threshold, '30')
  await user.click(
    screen.getByRole('button', { name: /save balance alert settings/i })
  )

  await waitFor(() => {
    expect(putMock).toHaveBeenCalledTimes(2)
  })
  expect(putMock.mock.calls[0][0]).toBe('/api/option/')
  expect(putMock.mock.calls[0][1]).toMatchObject({
    key: 'balance_alert_setting.enabled',
    value: true,
  })
  expect(putMock.mock.calls[1][1]).toMatchObject({
    key: 'balance_alert_setting.rules',
  })
  const rules = JSON.parse(putMock.mock.calls[1][1].value)
  expect(rules).toEqual([
    {
      id: 'rule-1',
      enabled: true,
      group: 'vip',
      threshold_amount: 30,
      max_users: 10,
      alert_toid: [123, 456],
      alert_url: 'https://example.com/hook',
    },
  ])
})
