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

import { CUSTOMER_VALIDATION, getCustomerFormErrorMessages } from '../constants'
import type { Customer, CustomerFormData } from '../types'

// ============================================================================
// Model Discount Row
// ============================================================================

export type PrefixDiscountRow = {
  prefix: string
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
      .min(
        CUSTOMER_VALIDATION.CUSTOMER_ID_MIN_LENGTH,
        msg.CUSTOMER_ID_REQUIRED
      ),
    name: z.string(),
    prefix_discounts: z.array(
      z.object({
        prefix: z.string(),
        discount: z.number(),
      })
    ),
    remark: z.string(),
  })
}

export type CustomerFormValues = {
  customer_id: string
  name: string
  prefix_discounts: PrefixDiscountRow[]
  remark: string
}

// ============================================================================
// Form Defaults
// ============================================================================

export const CUSTOMER_FORM_DEFAULT_VALUES: CustomerFormValues = {
  customer_id: '',
  name: '',
  prefix_discounts: [],
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
  const prefixDiscounts: Record<string, number> = {}
  for (const row of data.prefix_discounts) {
    const prefix = row.prefix.trim()
    if (!prefix) continue
    if (!Number.isFinite(row.discount) || row.discount <= 0) continue
    prefixDiscounts[prefix] = row.discount
  }

  return {
    customer_id: data.customer_id.trim(),
    name: data.name,
    remark: data.remark,
    prefix_discounts: prefixDiscounts,
  }
}

/**
 * Transform customer data to form defaults, hydrating the dynamic prefix
 * discount rows from the Record (sorted by prefix string for stable display).
 */
export function transformCustomerToFormDefaults(
  customer: Customer
): CustomerFormValues {
  const prefixDiscounts = Object.entries(customer.prefix_discounts ?? {})
    .map(([prefix, discount]) => ({ prefix, discount }))
    .sort((a, b) => a.prefix.localeCompare(b.prefix))

  return {
    customer_id: customer.customer_id,
    name: customer.name,
    prefix_discounts: prefixDiscounts,
    remark: customer.remark,
  }
}
