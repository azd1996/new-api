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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { getAIAssistantConfig } from '../api'

const { get } = vi.hoisted(() => ({
  get: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: {
    get,
  },
}))

const validConfig = {
  enabled: true,
  group: 'ai-assistant',
  default_model: 'gpt-4o',
  max_tokens: 2048,
  temperature: 0.7,
}

describe('AI assistant config API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  test('returns the assistant config on success', async () => {
    get.mockResolvedValue({
      data: { success: true, message: '', data: validConfig },
    })

    await expect(getAIAssistantConfig()).resolves.toEqual(validConfig)
    expect(get).toHaveBeenCalledWith('/ai-assistant/config')
  })

  test('rejects instead of resolving undefined when the backend serves HTML', async () => {
    get.mockResolvedValue({
      data: '<!doctype html><html><body>spa fallback</body></html>',
    })

    const error = await getAIAssistantConfig().catch((cause: Error) => cause)
    expect(error).toBeInstanceOf(Error)
    expect((error as Error).message).toContain('endpoint is unavailable')
  })

  test('rejects when the response has no data field', async () => {
    get.mockResolvedValue({ data: { success: true, message: '' } })

    await expect(getAIAssistantConfig()).rejects.toThrow(
      'endpoint is unavailable'
    )
  })
})
