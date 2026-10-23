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

// ============================================================================
// Validation Constants
// ============================================================================

export const CUSTOMER_VALIDATION = {
  CUSTOMER_ID_MIN_LENGTH: 1,
} as const

// ============================================================================
// Error Messages
// ============================================================================

// i18n keys; use t(ERROR_MESSAGES.xxx) when displaying. For form schema with
// interpolation use getCustomerFormErrorMessages(t).
export const ERROR_MESSAGES = {
  UNEXPECTED: 'An unexpected error occurred',
  LOAD_FAILED: 'Failed to load customers',
  SEARCH_FAILED: 'Failed to search customers',
  CREATE_FAILED: 'Failed to create customer',
  UPDATE_FAILED: 'Failed to update customer',
  DELETE_FAILED: 'Failed to delete customer',
  CUSTOMER_ID_REQUIRED: 'Customer ID is required',
  DISCOUNT_INVALID: 'Discount must be a positive number',
  MODEL_REQUIRED: 'Model name is required',
} as const

/** For form schema only: returns translated messages. */
export function getCustomerFormErrorMessages(t: TFunction) {
  return {
    CUSTOMER_ID_REQUIRED: t(ERROR_MESSAGES.CUSTOMER_ID_REQUIRED),
    DISCOUNT_INVALID: t(ERROR_MESSAGES.DISCOUNT_INVALID),
    MODEL_REQUIRED: t(ERROR_MESSAGES.MODEL_REQUIRED),
  } as const
}

// ============================================================================
// Success Messages (i18n keys; use t(SUCCESS_MESSAGES.xxx) when displaying)
// ============================================================================

export const SUCCESS_MESSAGES = {
  CUSTOMER_CREATED: 'Customer created successfully',
  CUSTOMER_UPDATED: 'Customer updated successfully',
  CUSTOMER_DELETED: 'Customer deleted successfully',
} as const
