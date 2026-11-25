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

import { describe, expect, it } from 'vitest'

import { diffLines } from '../line-diff'

describe('diffLines', () => {
  it('returns empty for identical texts', () => {
    expect(diffLines('a\nb', 'a\nb')).toEqual([])
  })

  it('marks added and removed lines', () => {
    const lines = diffLines('a\nb\nc', 'a\nB\nc')
    expect(lines).toEqual([
      { type: 'context', text: 'a' },
      { type: 'removed', text: 'b' },
      { type: 'added', text: 'B' },
      { type: 'context', text: 'c' },
    ])
  })

  it('collapses long unchanged regions into a single ellipsis', () => {
    const before = ['l1', 'l2', 'l3', 'l4', 'l5', 'l6', 'old', 'l7', 'l8', 'l9', 'l10', 'l11'].join('\n')
    const after = before.replace('old', 'new')
    const lines = diffLines(before, after, 1)
    expect(lines).toEqual([
      { type: 'context', text: '...' },
      { type: 'context', text: 'l6' },
      { type: 'removed', text: 'old' },
      { type: 'added', text: 'new' },
      { type: 'context', text: 'l7' },
      { type: 'context', text: '...' },
    ])
  })

  it('falls back to full replace for very large inputs', () => {
    const big = Array.from({ length: 3000 }, (_, index) => `line-${index}`).join('\n')
    const other = Array.from({ length: 3000 }, (_, index) => `other-${index}`).join('\n')
    const lines = diffLines(big, other)
    expect(lines.filter((line) => line.type === 'removed')).toHaveLength(3000)
    expect(lines.filter((line) => line.type === 'added')).toHaveLength(3000)
  })
})
