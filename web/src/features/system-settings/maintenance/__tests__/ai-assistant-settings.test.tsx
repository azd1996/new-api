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
import { AIAssistantSettingsSection } from '../ai-assistant-settings-section'

function Fixture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <AIAssistantSettingsSection
          defaultValues={{
            'ai_assistant_setting.enabled': false,
            'ai_assistant_setting.group': '',
            'ai_assistant_setting.default_model': '',
            'ai_assistant_setting.timeout_seconds': 60,
            'ai_assistant_setting.max_tokens': 2048,
            'ai_assistant_setting.temperature': 0.7,
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

test('saving writes the enable toggle as a boolean and the assistant fields', async () => {
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

  await user.type(
    await screen.findByPlaceholderText('ai-assistant'),
    'ai-assistant'
  )
  await user.type(screen.getByPlaceholderText('gpt-4o'), 'gpt-4o')
  await user.click(
    screen.getByRole('switch', { name: 'Enable the AI assistant' })
  )
  await user.click(
    screen.getByRole('button', { name: /save ai assistant settings/i })
  )

  await waitFor(() => {
    expect(putMock).toHaveBeenCalledTimes(6)
  })

  const requests = putMock.mock.calls.map((call) => call[1])
  expect(requests).toContainEqual(
    expect.objectContaining({
      key: 'ai_assistant_setting.enabled',
      value: true,
    })
  )
  expect(requests).toContainEqual(
    expect.objectContaining({
      key: 'ai_assistant_setting.group',
      value: 'ai-assistant',
    })
  )
  expect(requests).toContainEqual(
    expect.objectContaining({
      key: 'ai_assistant_setting.default_model',
      value: 'gpt-4o',
    })
  )
})
