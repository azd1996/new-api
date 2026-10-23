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
/**
 * Utility functions for customers
 */

export type ModelDiscountEntry = {
  model: string
  discount: number
}

/**
 * Normalize a customer's model_discounts map into a stable, sorted list of
 * entries for display (table summary chips, mobile list, tooltips).
 */
export function getModelDiscountEntries(
  modelDiscounts: Record<string, number> | undefined | null
): ModelDiscountEntry[] {
  if (!modelDiscounts) return []
  return Object.entries(modelDiscounts)
    .map(([model, discount]) => ({ model, discount }))
    .sort((a, b) => a.model.localeCompare(b.model))
}
