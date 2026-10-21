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
import { api } from '@/lib/api'

import type {
  ScheduleApiResponse,
  ScheduleDatasource,
  ScheduleDatasourcePayload,
  ScheduleDatasourceTestPayload,
  SchedulePreviewRequest,
  SchedulePreviewResult,
  ScheduleRule,
  ScheduleRulePayload,
  ScheduleSimpleResponse,
  ScheduleTemplate,
  ScheduleTemplatePayload,
} from './types'

const BASE = '/api/schedule'

export const SCHEDULE_DATASOURCE_KEY = ['schedule', 'datasource'] as const
export const SCHEDULE_TEMPLATE_KEY = ['schedule', 'template'] as const
export const SCHEDULE_RULE_KEY = ['schedule', 'rule'] as const

// ---------------------------------------------------------------------------
// Datasources
// ---------------------------------------------------------------------------

export async function listDatasources() {
  const res = await api.get<ScheduleApiResponse<ScheduleDatasource[] | null>>(
    `${BASE}/datasource`
  )
  return res.data
}

export async function createDatasource(body: ScheduleDatasourcePayload) {
  const res = await api.post<ScheduleApiResponse<ScheduleDatasource>>(
    `${BASE}/datasource`,
    body
  )
  return res.data
}

export async function updateDatasource(
  id: number,
  body: ScheduleDatasourcePayload
) {
  const res = await api.put<ScheduleApiResponse<ScheduleDatasource>>(
    `${BASE}/datasource/${id}`,
    body
  )
  return res.data
}

export async function deleteDatasource(id: number) {
  const res = await api.delete<ScheduleSimpleResponse>(
    `${BASE}/datasource/${id}`
  )
  return res.data
}

export async function testDatasource(body: ScheduleDatasourceTestPayload) {
  const res = await api.post<ScheduleSimpleResponse>(
    `${BASE}/datasource/test`,
    body
  )
  return res.data
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

export async function listTemplates() {
  const res = await api.get<ScheduleApiResponse<ScheduleTemplate[] | null>>(
    `${BASE}/template`
  )
  return res.data
}

export async function createTemplate(body: ScheduleTemplatePayload) {
  const res = await api.post<ScheduleApiResponse<ScheduleTemplate>>(
    `${BASE}/template`,
    body
  )
  return res.data
}

export async function updateTemplate(
  id: number,
  body: ScheduleTemplatePayload
) {
  const res = await api.put<ScheduleApiResponse<ScheduleTemplate>>(
    `${BASE}/template/${id}`,
    body
  )
  return res.data
}

export async function deleteTemplate(id: number) {
  const res = await api.delete<ScheduleSimpleResponse>(`${BASE}/template/${id}`)
  return res.data
}

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

export async function listRules() {
  const res = await api.get<ScheduleApiResponse<ScheduleRule[] | null>>(
    `${BASE}/rule`
  )
  return res.data
}

export async function createRule(body: ScheduleRulePayload) {
  const res = await api.post<ScheduleApiResponse<ScheduleRule>>(
    `${BASE}/rule`,
    body
  )
  return res.data
}

export async function updateRule(id: number, body: ScheduleRulePayload) {
  const res = await api.put<ScheduleApiResponse<ScheduleRule>>(
    `${BASE}/rule/${id}`,
    body
  )
  return res.data
}

export async function deleteRule(id: number) {
  const res = await api.delete<ScheduleSimpleResponse>(`${BASE}/rule/${id}`)
  return res.data
}

// ---------------------------------------------------------------------------
// Preview
// ---------------------------------------------------------------------------

export async function previewSchedule(body: SchedulePreviewRequest) {
  const res = await api.post<ScheduleApiResponse<SchedulePreviewResult>>(
    `${BASE}/preview`,
    body
  )
  return res.data
}
