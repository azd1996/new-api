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

// Structural JSON diff used by the cache-optimization test preview. It
// recursively compares the original and the optimized request body and
// reports one entry per changed leaf path, so a multi-megabyte Claude body
// with a few breakpoint tweaks renders as a handful of rows instead of a
// wall of line noise.

export type JsonDiffChangeType = 'added' | 'removed' | 'changed'

export type JsonDiffChange = {
  path: string
  type: JsonDiffChangeType
  before: unknown
  after: unknown
}

const MAX_CHANGES = 200

function formatValue(value: unknown): string {
  if (value === undefined) return 'undefined'
  if (value === null) return 'null'
  const text = typeof value === 'string' ? value : JSON.stringify(value)
  if (text === undefined) return String(value)
  return text.length > 120 ? `${text.slice(0, 117)}...` : text
}

export function formatJsonDiffValue(value: unknown): string {
  return formatValue(value)
}

function diffValues(
  path: string,
  before: unknown,
  after: unknown,
  changes: JsonDiffChange[]
) {
  if (changes.length >= MAX_CHANGES) return
  if (before === after) return
  const beforeObject = isPlainObject(before)
  const afterObject = isPlainObject(after)
  if (beforeObject && afterObject) {
    diffObjects(path, before as Record<string, unknown>, after as Record<string, unknown>, changes)
    return
  }
  if (Array.isArray(before) && Array.isArray(after)) {
    diffArrays(path, before, after, changes)
    return
  }
  changes.push({ path, type: 'changed', before, after })
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function diffObjects(
  path: string,
  before: Record<string, unknown>,
  after: Record<string, unknown>,
  changes: JsonDiffChange[]
) {
  const keys = [...new Set([...Object.keys(before), ...Object.keys(after)])]
  for (const key of keys) {
    if (changes.length >= MAX_CHANGES) return
    const childPath = path ? `${path}.${key}` : key
    const inBefore = Object.hasOwn(before, key)
    const inAfter = Object.hasOwn(after, key)
    if (!inBefore) {
      changes.push({ path: childPath, type: 'added', before: undefined, after: after[key] })
    } else if (!inAfter) {
      changes.push({ path: childPath, type: 'removed', before: before[key], after: undefined })
    } else {
      diffValues(childPath, before[key], after[key], changes)
    }
  }
}

function diffArrays(path: string, before: unknown[], after: unknown[], changes: JsonDiffChange[]) {
  // Compare by index. Cache optimization rewrites values in place (TTL,
  // breakpoint moves), so index-aligned comparison keeps paths stable; a
  // whole-tail insertion surfaces as index changes past the insertion point.
  const length = Math.max(before.length, after.length)
  for (let index = 0; index < length; index++) {
    if (changes.length >= MAX_CHANGES) return
    const childPath = `${path}[${index}]`
    if (index >= before.length) {
      changes.push({ path: childPath, type: 'added', before: undefined, after: after[index] })
    } else if (index >= after.length) {
      changes.push({ path: childPath, type: 'removed', before: before[index], after: undefined })
    } else {
      diffValues(childPath, before[index], after[index], changes)
    }
  }
}

// Returns the structural changes between two JSON documents, or null when
// either side is not valid JSON (the caller then falls back to line diff).
export function diffJsonTrees(before: string, after: string): JsonDiffChange[] | null {
  let beforeTree: unknown
  let afterTree: unknown
  try {
    beforeTree = JSON.parse(before)
    afterTree = JSON.parse(after)
  } catch {
    return null
  }
  const changes: JsonDiffChange[] = []
  diffValues('', beforeTree, afterTree, changes)
  return changes
}
