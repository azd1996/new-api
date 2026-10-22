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
/**
 * Type definitions for the detailed error stats feature (admin only)
 */

// Standard business response envelope: { success, message, data }
export interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

// Per-model breakdown of a single error signature.
export interface ErrorStatModel {
  model: string
  count: number
}

// Per-channel breakdown of a single error signature.
export interface ErrorStatChannel {
  channel_id: number
  channel_name: string
  count: number
}

// A single aggregated error signature row returned by /api/log/error-stats.
export interface ErrorStatItem {
  signature: string
  signature_hash: string
  // Admin-configured category ("简名"); "" means uncategorized.
  short_name: string
  count: number
  first_seen: number // unix seconds
  last_seen: number // unix seconds
  status_code: number
  models: ErrorStatModel[]
  channels: ErrorStatChannel[]
}

// Query parameters accepted by /api/log/error-stats. Timestamps are unix
// seconds (int64) to match the backend contract.
export interface ErrorStatsParams {
  start_timestamp: number
  end_timestamp: number
  status_code?: number
  model_name?: string
  channel?: number
  group?: string
  limit?: number
}

// Admin-configured signature -> short name mapping.
export interface ErrorAlias {
  id: number
  signature_hash: string
  signature: string
  short_name: string
  created_at: number
  updated_at: number
}

// Upsert payload for /api/error-alias.
export interface UpsertErrorAliasPayload {
  signature_hash: string
  signature: string
  short_name: string
}

// A single error-signature normalization rule. Rules are applied in order
// (top to bottom) as sequential regex replacements, so ORDER MATTERS.
export interface SignatureRule {
  name: string
  pattern: string
  replacement: string
  enabled: boolean
}

// An advisory normalization rule suggested by the backend for residual
// variable tokens still present in the current error signatures. Keys match
// the JSON contract exactly (snake_case match_count).
export interface SuggestedRule {
  name: string
  pattern: string
  replacement: string
  enabled: boolean
  // A sample signature that this suggested pattern matched.
  example: string
  // How many distinct signatures the pattern matched.
  match_count: number
}
