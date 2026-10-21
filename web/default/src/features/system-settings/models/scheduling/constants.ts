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
import type { ScheduleResultMode, ScheduleTargetType } from './types'

export const RESULT_MODES: ScheduleResultMode[] = ['rank', 'exclude']

export const TARGET_TYPES: ScheduleTargetType[] = ['group', 'user', 'token']

const PLACEHOLDER_REGEX = /__([A-Z0-9_]+)__/g

// extractPlaceholderNames returns the distinct placeholder names found in a
// template's PromQL, stripped of their surrounding underscores. A template may
// contain tokens like __MODEL__ or __TIMERANGE__; rules provide a value for
// each distinct name.
export function extractPlaceholderNames(promql: string): string[] {
  const names = new Set<string>()
  for (const match of promql.matchAll(PLACEHOLDER_REGEX)) {
    names.add(match[1])
  }
  return [...names]
}

// parseRuleParams safely parses a rule's `params` JSON string into a
// placeholderName -> value map. Invalid or non-object JSON yields an empty map.
export function parseRuleParams(params: string): Record<string, string> {
  if (!params.trim()) return {}
  try {
    const parsed = JSON.parse(params)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
      return {}
    }
    const result: Record<string, string> = {}
    for (const [key, value] of Object.entries(parsed)) {
      result[key] =
        value === null || value === undefined ? '' : String(value)
    }
    return result
  } catch {
    return {}
  }
}
