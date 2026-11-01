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
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AxiosAdapter, InternalAxiosRequestConfig } from 'axios'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/http-client'
import { createAppQueryClient } from '@/lib/query-client'
import { useAuthStore } from '@/stores/auth-store'
import { usePricingPreferencesStore } from '@/stores/pricing-preferences-store'

import type { ModelPricingConfig } from '../api'
import { ModelPricingPanel } from '../model-pricing-panel'
import { pricingOptions } from '../pricing'

const originalAdapter = api.defaults.adapter
let client: QueryClient | undefined

afterEach(() => {
  client?.clear()
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.setUser(null)
  localStorage.clear()
  vi.restoreAllMocks()
})

const suggested =
  'tier("standard", p * 1 + c * 5 + cr * 0.1 + cc * 1.25 + cc1h * 2)'

function respond(config: InternalAxiosRequestConfig, data: unknown) {
  return { data, status: 200, statusText: 'OK', headers: {}, config }
}

it('fills the draft from models.dev without saving until the administrator saves', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'administrator', role: 100 })
  usePricingPreferencesStore.setState({ currency: 'USD' })
  const values = {
    'billing_setting.billing_mode': 'tiered_expr',
    'billing_setting.billing_expr': 'tier("standard", p * 2 + c * 10)',
  }
  const snapshot: ModelPricingConfig = {
    entries: [
      {
        model_name: 'claude-haiku-4-5',
        version: 'v1',
        configured: values,
        effective: values,
      },
    ],
    options: pricingOptions({}),
    empty_version: 'empty',
  }
  const suggestionRequests: unknown[] = []
  const saves: string[] = []
  const adapter: AxiosAdapter = async (config) => {
    if (config.method === 'get' && config.url === '/api/status') {
      return respond(config, { success: true, data: {} })
    }
    if (config.method === 'get' && config.url === '/api/pricing') {
      return respond(config, { success: true, data: [], vendors: [] })
    }
    if (config.method === 'get' && config.url === '/api/option/model_pricing') {
      return respond(config, { success: true, data: snapshot })
    }
    if (
      config.method === 'get' &&
      config.url === '/api/ratio_sync/models_dev_suggestion'
    ) {
      suggestionRequests.push(config.params)
      return respond(config, {
        success: true,
        data: {
          expression: suggested,
          provider: 'anthropic',
          source_model: 'claude-haiku-4-5',
          derived_fields: ['cc1h'],
        },
      })
    }
    if (
      config.method === 'patch' &&
      config.url === '/api/option/model_pricing'
    ) {
      saves.push(String(config.data))
      return respond(config, { success: true })
    }
    throw new Error(`Unexpected request: ${config.method} ${config.url}`)
  }
  api.defaults.adapter = adapter
  client = createAppQueryClient()
  render(
    <QueryClientProvider client={client}>
      <ModelPricingPanel modelName='claude-haiku-4-5' />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  const price = await screen.findByRole('textbox', { name: 'Input price' })
  expect(price).toHaveValue('2')

  await user.click(screen.getByRole('button', { name: 'Fill from models.dev' }))

  // The expression editor remounts with the filled draft.
  await waitFor(() =>
    expect(screen.getByRole('textbox', { name: 'Input price' })).toHaveValue(
      '1'
    )
  )
  expect(suggestionRequests).toEqual([{ model: 'claude-haiku-4-5' }])
  expect(saves).toHaveLength(0)

  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() => expect(saves).toHaveLength(1))
  expect(saves[0]).toContain('p * 1 + c * 5')
})
