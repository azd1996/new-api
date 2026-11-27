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

// ============================================================================
// Session Identity Schema & Types
// ============================================================================

const sessionIdentitySourceSchema = z.object({
  type: z.enum(['metadata_user_id', 'request_header', 'gjson', 'regex']),
  key: z
    .string()
    .nullish()
    .transform((value) => value ?? ''),
  path: z
    .string()
    .nullish()
    .transform((value) => value ?? ''),
  json_string_field: z
    .string()
    .nullish()
    .transform((value) => value ?? ''),
  pattern: z
    .string()
    .nullish()
    .transform((value) => value ?? ''),
  value_regex: z.string().optional(),
  capture_group: z
    .number()
    .int()
    .min(0)
    .nullish()
    .transform((value) => value ?? 0),
})

export const sessionIdentitySchema = z.object({
  enabled: z.boolean(),
  model_prefixes: z
    .array(z.string())
    .nullable()
    .transform((value) => value ?? []),
  sources: z
    .array(sessionIdentitySourceSchema)
    .nullable()
    .transform((value) => value ?? []),
  transform: z
    .string()
    .nullish()
    .transform((value) => value ?? ''),
  value_regex: z
    .string()
    .nullish()
    .transform((value) => value ?? ''),
  omit_system_prefix: z.boolean().optional(),
  on_empty: z
    .string()
    .nullish()
    .transform((value) => value ?? 'legacy'),
  disable_first_message_fallback: z
    .boolean()
    .nullish()
    .transform((value) => value ?? false),
  ttl_seconds: z
    .number()
    .int()
    .min(0)
    .nullish()
    .transform((value) => value ?? 0),
})

export type SessionIdentitySource = z.infer<typeof sessionIdentitySourceSchema>
export type SessionIdentity = z.infer<typeof sessionIdentitySchema>

export const customerSchema = z.object({
  id: z.number(),
  customer_id: z.string(), // unique business id (the downstream api_key_id)
  name: z.string(),
  remark: z.string(),
  prefix_discounts: z.record(z.string(), z.number()), // model-name prefix -> multiplier
  created_time: z.number(),
  updated_time: z.number(),
  claude_cache_optimization: claudeCacheOptimizationSchema.nullish(),
  session_identity: sessionIdentitySchema.nullish(),
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
  session_identity?: SessionIdentity
  session_routing_model_prefixes?: Record<string, boolean>
}

// ============================================================================
// Dialog Types
// ============================================================================

export type CustomersDialogType = 'create' | 'update' | 'delete' | 'stats'
