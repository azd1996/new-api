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
import { buildQueryParams } from '@/features/usage-logs/lib/utils'
import { api } from '@/lib/api'

import type {
  ApiEnvelope,
  ErrorAlias,
  ErrorStatItem,
  ErrorStatsParams,
  SignatureRule,
  SuggestedRule,
  UpsertErrorAliasPayload,
} from './types'

// ============================================================================
// Detailed Error Stats APIs (admin only)
// ============================================================================

// Fetch aggregated error signatures for the given time range and filters.
export async function getErrorStats(
  params: ErrorStatsParams
): Promise<ApiEnvelope<ErrorStatItem[]>> {
  const query = buildQueryParams(params as unknown as Record<string, unknown>)
  const res = await api.get(`/api/log/error-stats?${query}`)
  return res.data
}

// List all admin-configured signature -> short name mappings.
export async function getErrorAliases(): Promise<ApiEnvelope<ErrorAlias[]>> {
  const res = await api.get('/api/error-alias')
  return res.data
}

// Upsert the short name (简名) for a signature.
export async function upsertErrorAlias(
  payload: UpsertErrorAliasPayload
): Promise<ApiEnvelope<null>> {
  const res = await api.post('/api/error-alias', payload)
  return res.data
}

// Remove the mapping for a signature hash (reverts to uncategorized).
export async function deleteErrorAlias(
  hash: string
): Promise<ApiEnvelope<null>> {
  const res = await api.delete(`/api/error-alias/${encodeURIComponent(hash)}`)
  return res.data
}

// ============================================================================
// Error signature normalization rules (admin only)
// ============================================================================

// Fetch the ordered set of error-signature normalization rules. Returns the
// admin-configured set, or the built-in defaults if none configured.
export async function getErrorSignatureRules(): Promise<
  ApiEnvelope<SignatureRule[]>
> {
  const res = await api.get('/api/log/error-signature-rules')
  return res.data
}

// Persist the ordered set of normalization rules. The backend validates and
// compiles every pattern; on an invalid regex it persists nothing and returns
// an error message identifying the rejected pattern.
export async function updateErrorSignatureRules(
  rules: SignatureRule[]
): Promise<ApiEnvelope<SignatureRule[]>> {
  const res = await api.put('/api/log/error-signature-rules', { rules })
  return res.data
}

// Fetch advisory normalization-rule suggestions for the same window/filter the
// stats page already sends to getErrorStats. Each suggestion targets residual
// variable tokens still present in the current error signatures.
export async function getErrorSignatureRuleSuggestions(
  params: ErrorStatsParams
): Promise<ApiEnvelope<SuggestedRule[]>> {
  const query = buildQueryParams(params as unknown as Record<string, unknown>)
  const res = await api.get(
    `/api/log/error-signature-rules/suggestions?${query}`
  )
  return res.data
}
