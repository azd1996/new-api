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
  ApiResponse,
  Customer,
  CustomerFormData,
  GetCustomersParams,
  GetCustomersResponse,
  SearchCustomersParams,
} from './types'

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
