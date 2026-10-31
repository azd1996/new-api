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
import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import type { CacheSkeleton, SkeletonTurn } from '../../types'
import {
  buildSkeletonGroups,
  buildTurnTimeline,
  skeletonLosses,
  skeletonRate,
} from '../cache-skeleton'

function skeleton(overrides: Partial<CacheSkeleton>): CacheSkeleton {
  return {
    ts: '',
    end_ts: '',
    request_id: 'r1',
    attempt: 0,
    status: 'ok',
    node: 'n1',
    session_id: 's1',
    lane: '0',
    channel_id: 1,
    key_idx: 0,
    model: 'claude',
    param_fp: '0',
    thinking: '',
    effort: '',
    blk_hash: [],
    blk_kind: [],
    blk_name: [],
    blk_len: [],
    msg_start: [],
    bp_idx: [],
    bp_ttl: [],
    fixed: '',
    cache_read: 0,
    cache_creation: 0,
    input: 0,
    ...overrides,
  }
}

// tools(1) + system(1) + user text, assistant tool_use, user tool_result,
// trailing mid-conversation system reminder carrying the breakpoint.
const reminderTail = skeleton({
  blk_hash: ['a1', 'b2', 'c3', 'd4', 'e5', 'f6'].map((h) => h.padEnd(16, '0')),
  blk_kind: [
    'tool',
    'system.text',
    'user.text',
    'assistant.tool_use',
    'user.tool_result',
    'system.text',
  ],
  blk_name: ['Bash', '', '', 'Bash', '', ''],
  blk_len: [100, 200, 10, 20, 30, 40],
  msg_start: [2, 3, 4, 5],
  bp_idx: [1, 5],
  bp_ttl: ['1h', '5m'],
})

describe('cache skeleton layout', () => {
  test('groups blocks into tools, system and one group per message', () => {
    const groups = buildSkeletonGroups(reminderTail)

    assert.deepEqual(
      groups.map((g) => [g.key, g.label, g.rows.length]),
      [
        ['tools', 'tools', 1],
        ['system', 'system', 1],
        ['m0', 'user', 1],
        ['m1', 'assistant', 1],
        ['m2', 'user', 1],
        ['m3', 'system', 1],
      ]
    )
    assert.equal(groups[3].rows[0].type, 'tool_use')
    assert.equal(groups[3].rows[0].name, 'Bash')
  })

  test('accumulates byte offsets across groups', () => {
    const rows = buildSkeletonGroups(reminderTail).flatMap((g) => g.rows)

    assert.deepEqual(
      rows.map((r) => r.cumulativeBytes),
      [100, 300, 310, 330, 360, 400]
    )
  })

  test('flags only a breakpoint on a trailing system message', () => {
    const rows = buildSkeletonGroups(reminderTail).flatMap((g) => g.rows)

    assert.equal(rows[1].breakpointTtl, '1h')
    assert.equal(rows[1].tailSystemBreakpoint, false)
    assert.equal(rows[5].breakpointTtl, '5m')
    assert.equal(rows[5].tailSystemBreakpoint, true)
  })

  test('does not flag a breakpoint on a trailing tool_result', () => {
    const fixed = skeleton({
      ...reminderTail,
      blk_hash: reminderTail.blk_hash.slice(0, 5),
      blk_kind: reminderTail.blk_kind.slice(0, 5),
      blk_name: reminderTail.blk_name.slice(0, 5),
      blk_len: reminderTail.blk_len.slice(0, 5),
      msg_start: [2, 3, 4],
      bp_idx: [4],
      bp_ttl: ['5m'],
    })
    const rows = buildSkeletonGroups(fixed).flatMap((g) => g.rows)

    assert.equal(rows[4].breakpointTtl, '5m')
    assert.equal(rows[4].tailSystemBreakpoint, false)
  })
})

function turn(overrides: Partial<SkeletonTurn>): SkeletonTurn {
  return {
    request_id: 'r1',
    ts: '',
    status: 'ok',
    attempt: 0,
    channel_id: 1,
    key_idx: 0,
    gap_s: 0,
    total_tokens: 0,
    actual_read: 0,
    expected_current: 0,
    expected_adjusted: 0,
    expected_adjusted_1h: 0,
    reason: 'hit',
    diverge_idx: -1,
    tail_volatile: false,
    total_bytes: 0,
    segments: null,
    breakpoints: null,
    ...overrides,
  }
}

describe('session cache diagnosis', () => {
  test('rate is a one-decimal percentage and a dash without input', () => {
    assert.equal(skeletonRate(1, 3), '33.3%')
    assert.equal(skeletonRate(5, 0), '—')
  })

  test('losses split the gap into upstream, breakpoint and TTL steps', () => {
    const losses = skeletonLosses({
      total: 1000,
      actual: 200,
      current: 500,
      adjusted: 800,
      adjusted_1h: 900,
    })

    assert.deepEqual(losses, { upstream: 300, misplaced: 300, ttl: 100 })
  })

  test('losses never go negative when actual exceeds the simulation', () => {
    const losses = skeletonLosses({
      total: 1000,
      actual: 600,
      current: 500,
      adjusted: 500,
      adjusted_1h: 500,
    })

    assert.equal(losses.upstream, 0)
  })

  test('timeline scales segments and breakpoints to the largest turn', () => {
    const bar = buildTurnTimeline(
      turn({
        total_bytes: 500,
        segments: [
          { state: 'same', bytes: 300 },
          { state: 'new', bytes: 200 },
        ],
        breakpoints: [{ offset: 300, ttl: '5m' }],
      }),
      1000
    )

    assert.deepEqual(
      bar.segments.map((s) => [s.state, s.widthPct]),
      [
        ['same', 30],
        ['new', 20],
      ]
    )
    assert.deepEqual(bar.breakpoints, [{ leftPct: 30, ttl: '5m' }])
  })

  test('timeline of a failed turn without segments is empty', () => {
    const bar = buildTurnTimeline(turn({ status: 'error' }), 0)

    assert.deepEqual(bar, { segments: [], breakpoints: [] })
  })
})
