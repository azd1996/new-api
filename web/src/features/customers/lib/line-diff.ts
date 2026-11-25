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

// Line-level diff for the cache-optimization test preview. Both sides are
// pretty-printed JSON, so an LCS line diff with a few context lines reads
// like a git diff. Implemented locally to avoid pulling in a dependency for
// one call site.

export type DiffLineType = 'context' | 'added' | 'removed'

export type DiffLine = {
  type: DiffLineType
  text: string
}

type DiffOp = { type: 'equal' | 'insert' | 'delete'; text: string }

function diffOps(beforeLines: string[], afterLines: string[]): DiffOp[] {
  const beforeLength = beforeLines.length
  const afterLength = afterLines.length
  // LCS table. Pretty-printed JSON bodies can reach hundreds of thousands of
  // lines; guard the quadratic table with a cap and fall back to a plain
  // replace when exceeded.
  const maxTableCells = 4_000_000
  if (beforeLength * afterLength > maxTableCells) {
    return [
      ...beforeLines.map((text) => ({ type: 'delete' as const, text })),
      ...afterLines.map((text) => ({ type: 'insert' as const, text })),
    ]
  }
  const table: Uint32Array[] = Array.from(
    { length: beforeLength + 1 },
    () => new Uint32Array(afterLength + 1)
  )
  for (let i = beforeLength - 1; i >= 0; i--) {
    for (let j = afterLength - 1; j >= 0; j--) {
      table[i][j] =
        beforeLines[i] === afterLines[j]
          ? table[i + 1][j + 1] + 1
          : Math.max(table[i + 1][j], table[i][j + 1])
    }
  }
  const ops: DiffOp[] = []
  let i = 0
  let j = 0
  while (i < beforeLength && j < afterLength) {
    if (beforeLines[i] === afterLines[j]) {
      ops.push({ type: 'equal', text: beforeLines[i] })
      i++
      j++
    } else if (table[i + 1][j] >= table[i][j + 1]) {
      ops.push({ type: 'delete', text: beforeLines[i] })
      i++
    } else {
      ops.push({ type: 'insert', text: afterLines[j] })
      j++
    }
  }
  while (i < beforeLength) {
    ops.push({ type: 'delete', text: beforeLines[i] })
    i++
  }
  while (j < afterLength) {
    ops.push({ type: 'insert', text: afterLines[j] })
    j++
  }
  return ops
}

// Collapses the full op list into hunks with `contextLines` of surrounding
// equal lines, like `diff -U`. Returns an empty array when the texts match.
export function diffLines(before: string, after: string, contextLines = 3): DiffLine[] {
  if (before === after) return []
  const beforeLines = before.split('\n')
  const afterLines = after.split('\n')
  const ops = diffOps(beforeLines, afterLines)
  const keep = Array.from({ length: ops.length }, () => false)
  let hasChange = false
  for (let index = 0; index < ops.length; index++) {
    if (ops[index].type === 'equal') continue
    hasChange = true
    const start = Math.max(0, index - contextLines)
    const end = Math.min(ops.length - 1, index + contextLines)
    for (let mark = start; mark <= end; mark++) keep[mark] = true
  }
  if (!hasChange) return []
  const lines: DiffLine[] = []
  let skipping = false
  for (let index = 0; index < ops.length; index++) {
    if (!keep[index]) {
      if (!skipping) {
        lines.push({ type: 'context', text: '...' })
        skipping = true
      }
      continue
    }
    skipping = false
    const op = ops[index]
    if (op.type === 'equal') {
      lines.push({ type: 'context', text: op.text })
    } else if (op.type === 'insert') {
      lines.push({ type: 'added', text: op.text })
    } else {
      lines.push({ type: 'removed', text: op.text })
    }
  }
  return lines
}
