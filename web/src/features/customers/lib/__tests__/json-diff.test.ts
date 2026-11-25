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

import { diffJsonTrees, formatJsonDiffValue } from '../json-diff'

describe('diffJsonTrees', () => {
  it('returns empty list for identical documents', () => {
    const body = JSON.stringify({ model: 'claude', messages: [{ role: 'user' }] })
    expect(diffJsonTrees(body, body)).toEqual([])
  })

  it('reports changed leaf paths with before and after values', () => {
    const changes = diffJsonTrees(
      JSON.stringify({ model: 'a', ttl: '5m' }),
      JSON.stringify({ model: 'a', ttl: '1h' })
    )
    expect(changes).toEqual([
      { path: 'ttl', type: 'changed', before: '5m', after: '1h' },
    ])
  })

  it('reports added and removed keys', () => {
    const changes = diffJsonTrees(
      JSON.stringify({ keep: 1, drop: 2 }),
      JSON.stringify({ keep: 1, extra: true })
    )
    expect(changes).toEqual([
      { path: 'drop', type: 'removed', before: 2, after: undefined },
      { path: 'extra', type: 'added', before: undefined, after: true },
    ])
  })

  it('compares nested arrays by index and keeps object paths stable', () => {
    const before = {
      messages: [
        { role: 'user', content: [{ type: 'text', text: 'hi' }] },
      ],
    }
    const after = {
      messages: [
        {
          role: 'user',
          content: [
            { type: 'text', text: 'hi', cache_control: { type: 'ephemeral', ttl: '1h' } },
          ],
        },
      ],
    }
    const changes = diffJsonTrees(JSON.stringify(before), JSON.stringify(after))
    expect(changes).toEqual([
      {
        path: 'messages[0].content[0].cache_control',
        type: 'added',
        before: undefined,
        after: { type: 'ephemeral', ttl: '1h' },
      },
    ])
  })

  it('returns null when either side is not valid JSON', () => {
    expect(diffJsonTrees('not json', '{}')).toBeNull()
    expect(diffJsonTrees('{}', 'not json')).toBeNull()
  })

  it('caps the number of reported changes', () => {
    const before: Record<string, number> = {}
    const after: Record<string, number> = {}
    for (let index = 0; index < 500; index++) {
      before[`k${index}`] = index
      after[`k${index}`] = index + 1
    }
    const changes = diffJsonTrees(JSON.stringify(before), JSON.stringify(after))
    expect(changes).toHaveLength(200)
  })
})

describe('formatJsonDiffValue', () => {
  it('formats scalars and truncates long strings', () => {
    expect(formatJsonDiffValue(null)).toBe('null')
    expect(formatJsonDiffValue('text')).toBe('text')
    expect(formatJsonDiffValue({ ttl: '1h' })).toBe('{"ttl":"1h"}')
    const long = 'x'.repeat(200)
    expect(formatJsonDiffValue(long)).toBe(`${'x'.repeat(117)}...`)
  })
})
