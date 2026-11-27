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
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { LogSettingsSection } from '../log-settings-section'

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <LogSettingsSection
          defaultValues={{
            LogConsumeEnabled: true,
            'log_setting.user_export_enabled': true,
            'log_setting.max_range_days': 30,
          }}
        />
      </SettingsPageProvider>
    </>
  )
}

async function renderSettings() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Fixture />
    </QueryClientProvider>
  )
  return screen.findByRole('spinbutton', {
    name: 'Longest log time range (days)',
  })
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/performance/logs'
          ? { enabled: true, log_dir: '/var/log/new-api', file_count: 3 }
          : null,
    },
  }))
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

test('the page shows the four log modules in order, each with its own controls', async () => {
  await renderSettings()

  expect(
    screen.getAllByRole('heading', { level: 4 }).map((h) => h.textContent)
  ).toEqual([
    'Log recording',
    'Log search and export',
    'Clean history logs',
    'Server Log Management',
  ])
  const recording = screen.getByRole('group', { name: 'Log recording' })
  expect(within(recording).getAllByRole('switch')).toHaveLength(1)
  expect(
    within(recording).getByRole('switch', { name: 'Record quota usage' })
  ).toBeVisible()
  const search = screen.getByRole('group', { name: 'Log search and export' })
  expect(
    within(search).getByRole('switch', { name: 'Allow users to export logs' })
  ).toBeVisible()
  expect(
    within(search).getByRole('spinbutton', {
      name: 'Longest log time range (days)',
    })
  ).toBeVisible()
  const cleanup = screen.getByRole('group', { name: 'Clean history logs' })
  expect(
    within(cleanup).getByRole('button', { name: 'Clean logs' })
  ).toBeVisible()
  const server = screen.getByRole('group', { name: 'Server Log Management' })
  expect(
    await within(server).findByRole('button', { name: 'Clean Up Log Files' })
  ).toBeVisible()
})

test('history cleanup is disabled and clicking it cannot start a task', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  await renderSettings()
  const cleanup = screen.getByRole('group', { name: 'Clean history logs' })
  const button = within(cleanup).getByRole('button', { name: 'Clean logs' })

  expect(button).toBeDisabled()
  await user.click(button)
  fireEvent.click(button)

  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  expect(post).not.toHaveBeenCalled()
})

test('an existing history cleanup still shows its progress without allowing another task', async () => {
  const user = userEvent.setup()
  const post = vi.spyOn(api, 'post')
  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      data: {
        task_id: 'systask_1',
        status: 'running',
        state: { total: 40, processed: 10, progress: 25, remaining: 30 },
      },
    },
  })
  await renderSettings()
  const cleanup = screen.getByRole('group', { name: 'Clean history logs' })

  expect(
    await within(cleanup).findByText('10 of 40 log entries processed.')
  ).toBeVisible()
  expect(
    within(cleanup).getByRole('button', { name: 'Cleaning...' })
  ).toBeDisabled()
  await user.click(within(cleanup).getByRole('button', { name: 'Cleaning...' }))
  expect(post).not.toHaveBeenCalled()
})

test('saving sends only the log export settings that changed', async () => {
  const user = userEvent.setup()
  const days = await renderSettings()
  expect(days).toHaveValue(30)

  await user.click(
    screen.getByRole('switch', { name: 'Allow users to export logs' })
  )
  fireEvent.change(days, { target: { value: '90' } })
  await user.click(screen.getByRole('button', { name: 'Save log settings' }))

  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(2))
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: 'log_setting.user_export_enabled',
    value: false,
  })
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: 'log_setting.max_range_days',
    value: 90,
  })
})

test('a time range under one day shows a field error and is not saved', async () => {
  const user = userEvent.setup()
  const days = await renderSettings()

  fireEvent.change(days, { target: { value: '0' } })
  await user.click(screen.getByRole('button', { name: 'Save log settings' }))

  await waitFor(() => expect(days).toHaveAttribute('aria-invalid', 'true'))
  expect(api.put).not.toHaveBeenCalled()
})
