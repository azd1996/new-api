import type {
  LogAnalysisResult,
  SessionCacheRangeAnalysisResponse,
} from '@/features/usage-logs/types'
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
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import type {
  ApiResponse,
  ClaudeCacheOptimization,
  Customer,
  CustomerFormData,
  GetCustomersParams,
  GetCustomersResponse,
  SearchCustomersParams,
  SessionIdentity,
} from './types'

// ============================================================================
// Session Identity Dry-Run
// ============================================================================

export interface SessionIdentityTestItem {
  body: string
  headers?: Record<string, string>
  api_key?: string
  client_ip?: string
  user_agent?: string
}

export interface SessionIdentityTestResult {
  index: number
  ok: boolean
  session_id: string
  raw_value?: string
  source: string
  error?: string
}

export interface SessionIdentityTestSummary {
  count: number
  ok_count: number
  distinct_session_ids: number
  session_ids: string[]
  stable: boolean
  expect_same_met: boolean
  sources_used: Record<string, number>
}

export interface SessionIdentityTestResponse {
  results: SessionIdentityTestResult[]
  summary: SessionIdentityTestSummary
}

export interface SessionIdentityTestParams {
  customer_id?: string
  config?: SessionIdentity
  items: SessionIdentityTestItem[]
  expect_same?: boolean
}

export async function testCustomerSessionIdentity(
  params: SessionIdentityTestParams
): Promise<SessionIdentityTestResponse> {
  const response = await api.post<ApiResponse<SessionIdentityTestResponse>>(
    '/api/customer/session_identity/test',
    params
  )
  const result = requireServerSuccess(response.data)
  if (!result.data) {
    throw createServerError(
      result,
      'Failed to test session identity extraction'
    )
  }
  return result.data
}

// ============================================================================
// Claude Cache Optimization Dry-Run
// ============================================================================

export interface CacheOptimizationTestItem {
  body: string
}

export interface CacheOptimizationTestResult {
  index: number
  ok: boolean
  body?: string
  model?: string
  error?: string
  enabled_rules?: string[]
  changed_steps?: string[]
  skipped_steps?: string[]
  headers?: Record<string, string>
  // Full-relay preview fields.
  channel_id?: number
  channel_name?: string
  channel_type?: number
  group?: string
  origin_model?: string
  upstream_model?: string
  model_mapped?: boolean
  pass_through?: boolean
  url?: string
  method?: string
}

export interface CacheOptimizationTestResponse {
  results: CacheOptimizationTestResult[]
  mode: string
}

export interface CacheOptimizationTestParams {
  customer_id?: string
  config?: ClaudeCacheOptimization
  group?: string
  mode?: 'cache_optimization' | 'full_relay'
  items: CacheOptimizationTestItem[]
}

export async function testCustomerCacheOptimization(
  params: CacheOptimizationTestParams
): Promise<CacheOptimizationTestResponse> {
  const response = await api.post<ApiResponse<CacheOptimizationTestResponse>>(
    '/api/customer/cache_optimization/test',
    params
  )
  const result = requireServerSuccess(response.data)
  if (!result.data) {
    throw createServerError(result, 'Failed to test cache optimization')
  }
  return result.data
}

// ============================================================================
// Customer Management
// ============================================================================

// Get paginated customers list
export async function getCustomers(
  params: GetCustomersParams = {}
): Promise<GetCustomersResponse> {
  const { p = 1, page_size = 10 } = params
  const res = await api.get(`/api/customer/?p=${p}&page_size=${page_size}`)
  return res.data
}

// Search customers by keyword
export async function searchCustomers(
  params: SearchCustomersParams
): Promise<GetCustomersResponse> {
  const { keyword = '', p = 1, page_size = 10 } = params
  const queryParams = new URLSearchParams()
  queryParams.set('keyword', keyword)
  queryParams.set('p', String(p))
  queryParams.set('page_size', String(page_size))
  const res = await api.get(`/api/customer/search?${queryParams.toString()}`)
  return res.data
}

// Get single customer by ID
export async function getCustomer(id: number): Promise<ApiResponse<Customer>> {
  const res = await api.get(`/api/customer/${id}`)
  return res.data
}

// Create customer
export async function createCustomer(
  data: CustomerFormData
): Promise<ApiResponse<Customer>> {
  const res = await api.post('/api/customer/', data)
  return res.data
}

// Update customer (must include id)
export async function updateCustomer(
  data: CustomerFormData & { id: number }
): Promise<ApiResponse<Customer>> {
  const res = await api.put('/api/customer/', data)
  return res.data
}

// Delete a single customer
export async function deleteCustomer(id: number): Promise<ApiResponse> {
  const res = await api.delete(`/api/customer/${id}`)
  return res.data
}

export type CustomerStatisticsParams = {
  customer_id: string
  start_timestamp: number
  end_timestamp: number
}

export async function getCustomerLogAnalysis(
  params: CustomerStatisticsParams,
  signal?: AbortSignal
) {
  const response = await api.get<ApiResponse<LogAnalysisResult>>(
    '/api/log/analysis',
    { params, signal }
  )
  const result = requireServerSuccess(response.data)
  if (!result.data) throw createServerError(result, 'Failed to load analysis')
  return result.data
}

export async function getCustomerCacheAnalysis(
  params: CustomerStatisticsParams,
  signal?: AbortSignal
) {
  const response = await api.get<SessionCacheRangeAnalysisResponse>(
    '/api/log/cache-skeleton/sessions/analysis',
    { params, signal }
  )
  const result = requireServerSuccess(response.data)
  if (!result.data) throw createServerError(result, 'Failed to load analysis')
  return result.data
}
