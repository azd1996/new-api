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
import { describe, expect, test } from 'vitest'

import type { LogOtherData } from '../../types'
import {
  formatLatencyGapMs,
  formatLatencyMs,
  getUpstreamGapColor,
  getUpstreamTimingComparison,
} from '../format'

const bedrock = (frt_ms: number, total_ms: number): LogOtherData => ({
  frt: 7620,
  lat: 9400,
  admin_info: {
    upstream_latency: { source: 'aws_bedrock', frt_ms, total_ms },
  },
})

describe('upstream timing comparison', () => {
  test('gap is gateway-observed minus upstream-reported for both first token and total', () => {
    expect(getUpstreamTimingComparison(bedrock(7291, 9014), 9)).toEqual({
      sourceLabel: 'AWS',
      ourFirstTokenMs: 7620,
      ourTotalMs: 9400,
      upstreamFirstTokenMs: 7290,
      upstreamTotalMs: 9010,
      firstTokenGapMs: 330,
      totalGapMs: 390,
    })
  })

  test('total gap uses the precise lat, not the whole-second use_time', () => {
    // use_time rounds 1.3s up to 2s; comparing that with AWS 1.15s would
    // report a 0.85s gap that never happened.
    const other: LogOtherData = {
      frt: 650,
      lat: 1300,
      admin_info: {
        upstream_latency: { source: 'aws_bedrock', frt_ms: 520, total_ms: 1150 },
      },
    }
    const comparison = getUpstreamTimingComparison(other, 2)
    expect(comparison).toMatchObject({ ourTotalMs: 1300, upstreamTotalMs: 1150, totalGapMs: 150 })
    expect(formatLatencyMs(1300)).toBe('1.30s')
    expect(formatLatencyMs(1150)).toBe('1.15s')
    expect(formatLatencyGapMs(150)).toBe('+0.15s')
    expect(formatLatencyGapMs(-20)).toBe('−0.02s')
  })

  test('falls back to whole-second use_time when the precise total latency is missing', () => {
    const other = bedrock(7291, 9014)
    delete other.lat
    expect(getUpstreamTimingComparison(other, 10)?.totalGapMs).toBe(990)
  })

  test('returns null when the log has no upstream-reported timing', () => {
    expect(getUpstreamTimingComparison({ frt: 7620, lat: 9400 }, 9)).toBeNull()
    expect(getUpstreamTimingComparison(bedrock(0, 0), 9)).toBeNull()
  })

  test('leaves the first-token gap empty for logs without a gateway first token', () => {
    const other = bedrock(7291, 9014)
    delete other.frt
    const comparison = getUpstreamTimingComparison(other, 9)
    expect(comparison?.firstTokenGapMs).toBeNull()
    expect(comparison?.totalGapMs).toBe(390)
  })

  test('gap color escalates at one and three seconds', () => {
    expect(getUpstreamGapColor(-50)).toBe('success')
    expect(getUpstreamGapColor(999)).toBe('success')
    expect(getUpstreamGapColor(1000)).toBe('warning')
    expect(getUpstreamGapColor(3000)).toBe('danger')
  })
})
