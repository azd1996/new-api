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

const cacheOptimizationRuleSchema = z.object({
  enabled: z.boolean(),
  model_prefixes: z
    .array(z.string())
    .nullable()
    .transform((value) => value ?? []),
})

export const claudeCacheOptimizationSchema = z.object({
  force_extended_ttl: cacheOptimizationRuleSchema,
  fix_volatile_breakpoint: cacheOptimizationRuleSchema,
  retain_previous_breakpoint: cacheOptimizationRuleSchema,
  stable_base_breakpoint: cacheOptimizationRuleSchema,
  fallback_cache_breakpoint: cacheOptimizationRuleSchema.default({
    enabled: false,
    model_prefixes: [],
  }),
})

export type ClaudeCacheOptimization = z.infer<
  typeof claudeCacheOptimizationSchema
>

export const customerSchema = z.object({
  id: z.number(),
  customer_id: z.string(), // unique business id (the downstream api_key_id)
  name: z.string(),
  remark: z.string(),
  prefix_discounts: z.record(z.string(), z.number()), // model-name prefix -> multiplier
  created_time: z.number(),
  updated_time: z.number(),
  claude_cache_optimization: claudeCacheOptimizationSchema.nullish(),
  session_routing_model_prefixes: z.record(z.string(), z.boolean()).nullish(),
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
  prefix_discounts: Record<string, number>
  claude_cache_optimization?: ClaudeCacheOptimization
  session_routing_model_prefixes?: Record<string, boolean>
}

// ============================================================================
// Dialog Types
// ============================================================================

export type CustomersDialogType = 'create' | 'update' | 'delete' | 'stats'
