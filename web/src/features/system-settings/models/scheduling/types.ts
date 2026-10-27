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

// Result modes for a rule template. `rank` means the result rows express a
// priority order; `exclude` means the rows are entities to drop.
export type ScheduleResultMode = 'rank' | 'exclude'

// A scheduling rule can target a user group, a single user, or a token.
export type ScheduleTargetType = 'group' | 'user' | 'token'

export interface ScheduleDatasource {
  id: number
  name: string
  base_url: string
  username: string
  password_set: boolean
  enabled: boolean
  timeout_seconds: number
  created_time?: number
  updated_time?: number
}

export interface ScheduleDatasourcePayload {
  name: string
  base_url: string
  username: string
  password: string
  enabled: boolean
  timeout_seconds: number
}

export interface ScheduleDatasourceTestPayload {
  id?: number
  base_url: string
  username: string
  password: string
  timeout_seconds: number
}

export interface ScheduleTemplate {
  id: number
  name: string
  promql: string
  result_label: string
  result_mode: ScheduleResultMode
  description: string
  created_time?: number
  updated_time?: number
}

export interface ScheduleTemplatePayload {
  name: string
  promql: string
  result_label: string
  result_mode: ScheduleResultMode
  description: string
}

export interface ScheduleRule {
  id: number
  target_type: ScheduleTargetType
  target_value: string
  model: string
  template_id: number
  // params is a JSON STRING: an object of placeholderName -> value.
  params: string
  enabled: boolean
  created_time?: number
  updated_time?: number
}

export interface ScheduleRulePayload {
  target_type: ScheduleTargetType
  target_value: string
  model: string
  template_id: number
  params: string
  enabled: boolean
}

export interface SchedulePreviewRequest {
  datasource_id?: number
  rule_id?: number
  template_id?: number
  promql?: string
  result_label?: string
  params?: Record<string, string>
}

export interface SchedulePreviewRow {
  label: string
  value: number
  labels: Record<string, string>
}

export interface SchedulePreviewResult {
  resolved_promql: string
  rows: SchedulePreviewRow[]
}

export interface ScheduleApiResponse<T> {
  success: boolean
  message: string
  data: T
}

export interface ScheduleSimpleResponse {
  success: boolean
  message: string
}
