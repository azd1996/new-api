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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import {
  CUSTOMER_VALIDATION,
  getCustomerFormErrorMessages,
} from '../constants'
import type { Customer, CustomerFormData } from '../types'

// ============================================================================
// Model Discount Row
// ============================================================================

export type ModelDiscountRow = {
  model: string
  discount: number
}

// ============================================================================
// Form Schema (use getCustomerFormSchema(t) in components for i18n messages)
// ============================================================================

export function getCustomerFormSchema(t: TFunction) {
  const msg = getCustomerFormErrorMessages(t)
  return z.object({
    customer_id: z
      .string()
      .trim()
      .min(CUSTOMER_VALIDATION.CUSTOMER_ID_MIN_LENGTH, msg.CUSTOMER_ID_REQUIRED),
    name: z.string(),
    default_discount: z.number().positive(msg.DISCOUNT_INVALID),
    model_discounts: z.array(
      z.object({
        model: z.string(),
        discount: z.number(),
      })
    ),
    remark: z.string(),
  })
}

export type CustomerFormValues = {
  customer_id: string
  name: string
  default_discount: number
  model_discounts: ModelDiscountRow[]
  remark: string
}

// ============================================================================
// Form Defaults
// ============================================================================

export const CUSTOMER_FORM_DEFAULT_VALUES: CustomerFormValues = {
  customer_id: '',
  name: '',
  default_discount: 1,
  model_discounts: [],
  remark: '',
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload. Empty/invalid model discount rows are
 * dropped and the remaining rows are collapsed into a Record<string, number>.
 */
export function transformFormDataToPayload(
  data: CustomerFormValues
): CustomerFormData {
  const modelDiscounts: Record<string, number> = {}
  for (const row of data.model_discounts) {
    const model = row.model.trim()
    if (!model) continue
    if (!Number.isFinite(row.discount) || row.discount <= 0) continue
    modelDiscounts[model] = row.discount
  }

  return {
    customer_id: data.customer_id.trim(),
    name: data.name,
    remark: data.remark,
    default_discount: data.default_discount,
    model_discounts: modelDiscounts,
  }
}

/**
 * Transform customer data to form defaults, hydrating the dynamic model
 * discount rows from the Record.
 */
export function transformCustomerToFormDefaults(
  customer: Customer
): CustomerFormValues {
  const modelDiscounts = Object.entries(customer.model_discounts ?? {}).map(
    ([model, discount]) => ({ model, discount })
  )

  return {
    customer_id: customer.customer_id,
    name: customer.name,
    default_discount: customer.default_discount || 1,
    model_discounts: modelDiscounts,
    remark: customer.remark,
  }
}
