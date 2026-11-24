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
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export interface AIAssistantConfig {
  enabled: boolean
  group: string
  default_model: string
  max_tokens: number
  temperature: number
}

export async function getAIAssistantConfig(): Promise<AIAssistantConfig> {
  const res = await api.get<{
    success: boolean
    message: string
    data?: AIAssistantConfig
  }>('/ai-assistant/config')
  const body = requireServerSuccess(res.data)

  // A 2xx response without a JSON body (for example an SPA fallback HTML page
  // served for an unknown API path) makes `body.data` undefined. Returning
  // undefined would surface as React Query's cryptic "data is undefined"
  // error, so fail with an actionable message instead.
  if (typeof body !== 'object' || body === null || body.data === undefined) {
    throw new Error(
      'AI assistant config endpoint is unavailable. The backend must be rebuilt from the same version as the frontend (it needs GET /api/ai-assistant/config).'
    )
  }
  return body.data
}

export function useAIAssistantConfig() {
  return useQuery({
    queryKey: ['ai-assistant-config'],
    queryFn: getAIAssistantConfig,
  })
}
