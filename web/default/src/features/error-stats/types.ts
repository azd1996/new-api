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

// Per-customer breakdown of a single error signature. customer_name is the
// configured display name, or "" when the customer id has no mapping.
export interface ErrorStatCustomer {
  customer_id: string
  customer_name: string
  count: number
}

// How /api/log/error-stats aggregates its rows. `signature` (default) returns
// one row per distinct error signature; the grouped modes collapse rows by the
// resolved category ("简名") or attribution ("错误归因").
export type ErrorStatsGroupBy = 'signature' | 'short_name' | 'attribution'

// Ordered list of supported view modes for the stats page toggle.
export const ERROR_STATS_GROUP_BY_VALUES: readonly ErrorStatsGroupBy[] = [
  'signature',
  'short_name',
  'attribution',
]

// A single aggregated error row returned by /api/log/error-stats. The meaning
// of several fields depends on the active `group_by` mode (see field notes).
export interface ErrorStatItem {
  // Grouping identity. In signature mode this is the signature hash; in grouped
  // modes it is the category/attribution value ("" => uncategorized).
  group_key: string
  // Human-readable label for the group. In signature mode this is the signature
  // text; in grouped modes it is the category/attribution value.
  group_label: string
  // Only meaningful in signature mode; "" in grouped modes.
  signature: string
  // Only meaningful in signature mode; "" in grouped modes.
  signature_hash: string
  // Admin-configured category ("简名"); "" means uncategorized. In short_name
  // mode this carries the group value; in attribution mode it is "".
  short_name: string
  // Admin-configured attribution ("错误归因"). In attribution mode this carries
  // the group value; in short_name mode it is "".
  attribution: string
  // Admin-written free-text explanation of why the signature was categorized
  // this way. Only meaningful in signature mode; "" in grouped modes.
  note: string
  count: number
  first_seen: number // unix seconds
  last_seen: number // unix seconds
  // Only populated in signature mode.
  status_code: number
  models: ErrorStatModel[]
  channels: ErrorStatChannel[]
  customers: ErrorStatCustomer[]
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
  group_by?: ErrorStatsGroupBy
  limit?: number
}

// Admin-configured signature -> short name / attribution mapping.
export interface ErrorAlias {
  id: number
  signature_hash: string
  signature: string
  short_name: string
  attribution: string
  note: string
  created_at: number
  updated_at: number
}

// Upsert payload for /api/error-alias. The backend requires signature_hash plus
// at least one of short_name / attribution / note to be non-empty.
export interface UpsertErrorAliasPayload {
  signature_hash: string
  signature: string
  short_name: string
  attribution: string
  note: string
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
