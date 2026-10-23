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

export type PrefixDiscountEntry = {
  prefix: string
  discount: number
}

/**
 * Normalize a customer's prefix_discounts map into a stable, string-sorted list
 * of entries for display (table summary, drawer rows, tooltips).
 */
export function getPrefixDiscountEntries(
  prefixDiscounts: Record<string, number> | undefined | null
): PrefixDiscountEntry[] {
  if (!prefixDiscounts) return []
  return Object.entries(prefixDiscounts)
    .map(([prefix, discount]) => ({ prefix, discount }))
    .sort((a, b) => a.prefix.localeCompare(b.prefix))
}
