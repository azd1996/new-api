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
import type { CacheSkeleton, SkeletonRates, SkeletonTurn } from '../types'

export interface SkeletonBlockRow {
  index: number
  type: string
  name: string
  hash: string
  bytes: number
  cumulativeBytes: number
  breakpointTtl?: string
  // The breakpoint sits on the last message and that message is a
  // mid-conversation system message (e.g. a per-turn <total_tokens>
  // reminder), which the next turn usually moves or rewrites.
  tailSystemBreakpoint: boolean
}

export interface SkeletonGroup {
  key: string
  // 'tools' | 'system' | message role
  label: string
  messageIndex?: number
  rows: SkeletonBlockRow[]
}

// buildSkeletonGroups lays one skeleton out for reading: tools, system, then
// one group per message, each block with its running byte offset so the size
// of the prefix cut by every breakpoint is visible.
export function buildSkeletonGroups(item: CacheSkeleton): SkeletonGroup[] {
  const ttlByIndex = new Map<number, string>()
  item.bp_idx.forEach((idx, i) => ttlByIndex.set(idx, item.bp_ttl[i] ?? ''))
  const lastMessageStart = item.msg_start.at(-1) ?? -1
  const lastMessageIsSystem =
    lastMessageStart >= 0 &&
    (item.blk_kind[lastMessageStart] ?? '').startsWith('system.')

  const groups: SkeletonGroup[] = []
  let cumulative = 0
  let messageCursor = -1
  item.blk_hash.forEach((hash, index) => {
    const kind = item.blk_kind[index] ?? ''
    const [prefix, ...rest] = kind.split('.')
    const isMessage = item.msg_start.length > 0 && index >= item.msg_start[0]
    let groupKey = prefix === 'tool' ? 'tools' : 'system'
    let label = groupKey
    if (isMessage) {
      while (
        messageCursor + 1 < item.msg_start.length &&
        item.msg_start[messageCursor + 1] <= index
      ) {
        messageCursor++
      }
      groupKey = `m${messageCursor}`
      label = prefix
    }
    let group = groups.at(-1)
    if (!group || group.key !== groupKey) {
      group = {
        key: groupKey,
        label,
        messageIndex: isMessage ? messageCursor : undefined,
        rows: [],
      }
      groups.push(group)
    }
    const bytes = item.blk_len[index] ?? 0
    cumulative += bytes
    const breakpointTtl = ttlByIndex.get(index)
    group.rows.push({
      index,
      type: isMessage || prefix === 'system' ? rest.join('.') : prefix,
      name: item.blk_name[index] ?? '',
      hash: hash.slice(0, 8),
      bytes,
      cumulativeBytes: cumulative,
      breakpointTtl,
      tailSystemBreakpoint:
        breakpointTtl !== undefined &&
        lastMessageIsSystem &&
        index >= lastMessageStart,
    })
  })
  return groups
}

// formatBytes renders a byte count with a binary unit for the skeleton view.
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  return `${(bytes / 1024 / 1024).toFixed(1)}MB`
}

// i18n keys of the simulated miss reasons (model/cache_skeleton_analysis.go).
export const SKELETON_REASON_LABELS: Record<string, string> = {
  error: 'Failed request',
  first_turn: 'First turn',
  too_small: 'Below cache minimum',
  no_breakpoint: 'No breakpoint',
  param_changed: 'Parameters changed',
  prefix_changed: 'Tools or system changed',
  concurrent: 'Concurrent with previous turn',
  ttl_expired: 'TTL expired',
  breakpoint_misplaced: 'Breakpoint misplaced',
  lookback_exceeded: 'Cache lookback exceeded',
  channel_switch: 'Channel or key switched',
  upstream_miss: 'Upstream miss',
  hit: 'Hit as expected',
  cache_key_changed: 'prompt_cache_key changed',
  previous_response: 'Uses previous_response_id',
}

// Badge color per reason: gateway-recoverable causes warn, upstream-side
// misses are red, structural client changes are informational.
export const SKELETON_REASON_VARIANTS: Record<
  string,
  'success' | 'warning' | 'danger' | 'info' | 'neutral'
> = {
  error: 'danger',
  first_turn: 'neutral',
  too_small: 'neutral',
  no_breakpoint: 'info',
  param_changed: 'info',
  prefix_changed: 'info',
  concurrent: 'info',
  ttl_expired: 'warning',
  breakpoint_misplaced: 'warning',
  lookback_exceeded: 'warning',
  channel_switch: 'danger',
  upstream_miss: 'danger',
  hit: 'success',
  cache_key_changed: 'warning',
  previous_response: 'neutral',
}

export interface SkeletonTierLabels {
  current: string
  adjusted: string
  adjusted1h: string
  // Loss closed by the adjusted step (the TTL step is shared).
  adjustedLoss: string
}

// skeletonTierLabels names the three simulated ceilings of a lane (i18n
// keys). Claude caches at explicit breakpoints the gateway can move; OpenAI
// caches the longest prefix automatically and routes by prompt_cache_key.
export function skeletonTierLabels(provider: string): SkeletonTierLabels {
  if (provider === 'openai') {
    return {
      current: 'Prefix-match ceiling',
      adjusted: 'With one prompt_cache_key',
      adjusted1h: 'One key + 24h retention',
      adjustedLoss: 'prompt_cache_key changes',
    }
  }
  return {
    current: 'Ceiling with client breakpoints',
    adjusted: 'After breakpoint adjustment',
    adjusted1h: 'Adjustment + 1h TTL',
    adjustedLoss: 'Breakpoint misplaced',
  }
}

// skeletonRate is part/total as a percentage, '—' without input tokens.
export function skeletonRate(part: number, total: number): string {
  if (total <= 0) return '—'
  return `${((part / total) * 100).toFixed(1)}%`
}

export interface SkeletonLossBreakdown {
  // Expected by the first simulated ceiling, not read.
  upstream: number
  // Recovered by the adjusted step: the gateway breakpoint adjustment
  // (Claude) or one prompt_cache_key (OpenAI).
  adjusted: number
  // Recovered by additionally extending the TTL (1h / 24h).
  ttl: number
}

// skeletonLosses splits the gap between the actual and the best simulated
// hit tokens into the step that would close each part.
export function skeletonLosses(rates: SkeletonRates): SkeletonLossBreakdown {
  return {
    upstream: Math.max(0, rates.current - rates.actual),
    adjusted: Math.max(0, rates.adjusted - rates.current),
    ttl: Math.max(0, rates.adjusted_1h - rates.adjusted),
  }
}

export interface SkeletonUpstreamSplit {
  // Loss on turns where new-api itself switched channel or key.
  channelSwitch: number
  // Loss on turns whose channel and key held but upstream still missed.
  upstreamMiss: number
}

// skeletonUpstreamSplit divides the combined upstream loss into the gateway's
// own channel/key switches and genuine upstream misses, by summing each
// turn's unread ceiling tokens under its attributed reason.
export function skeletonUpstreamSplit(
  turns: SkeletonTurn[]
): SkeletonUpstreamSplit {
  const split: SkeletonUpstreamSplit = { channelSwitch: 0, upstreamMiss: 0 }
  for (const turn of turns) {
    const loss = Math.max(0, turn.expected_current - turn.actual_read)
    if (turn.reason === 'channel_switch') split.channelSwitch += loss
    else if (turn.reason === 'upstream_miss') split.upstreamMiss += loss
  }
  return split
}

export interface TimelineBar {
  segments: { state: string; widthPct: number }[]
  breakpoints: { leftPct: number; ttl: string }[]
}

// buildTurnTimeline scales one turn's segments and breakpoint offsets to the
// largest turn of its lane so prompt growth reads left to right.
export function buildTurnTimeline(
  turn: SkeletonTurn,
  scaleBytes: number
): TimelineBar {
  const scale = Math.max(scaleBytes, turn.total_bytes, 1)
  const pct = (bytes: number) => Math.min(100, (bytes / scale) * 100)
  return {
    segments: (turn.segments ?? []).map((s) => ({
      state: s.state,
      widthPct: pct(s.bytes),
    })),
    breakpoints: (turn.breakpoints ?? []).map((b) => ({
      leftPct: pct(b.offset),
      ttl: b.ttl,
    })),
  }
}
