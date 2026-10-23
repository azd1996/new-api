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
import { z } from 'zod'

// ============================================================================
// Customer Schema & Types
// ============================================================================

export const customerSchema = z.object({
  id: z.number(),
  customer_id: z.string(), // unique business id (the downstream api_key_id)
  name: z.string(),
  remark: z.string(),
  default_discount: z.number(), // stat discount multiplier (0.8 = pay 80%)
  model_discounts: z.record(z.string(), z.number()), // per-model multipliers
  created_time: z.number(),
  updated_time: z.number(),
})

export type Customer = z.infer<typeof customerSchema>

// ============================================================================
// API Request/Response Types
// ============================================================================

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetCustomersParams {
  p?: number
  page_size?: number
}

export interface GetCustomersResponse {
  success: boolean
  message?: string
  data?: {
    items: Customer[]
    total: number
    page: number
    page_size: number
  }
}

export interface SearchCustomersParams {
  keyword?: string
  p?: number
  page_size?: number
}

export interface CustomerFormData {
  id?: number
  customer_id: string
  name: string
  remark: string
  default_discount: number
  model_discounts: Record<string, number>
}

// ============================================================================
// Dialog Types
// ============================================================================

export type CustomersDialogType = 'create' | 'update' | 'delete'
