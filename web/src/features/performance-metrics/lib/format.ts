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
export function formatThroughput(tps: number): string {
  if (!Number.isFinite(tps) || tps <= 0) return '—'
  if (tps >= 1_000) return `${(tps / 1_000).toFixed(1)}K t/s`
  return `${tps.toFixed(tps < 10 ? 2 : 1)} t/s`
}

export function formatLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—'
  if (ms >= 1_000) return `${(ms / 1_000).toFixed(2)}s`
  return `${Math.round(ms)}ms`
}

export function formatUptimePct(pct: number): string {
  if (!Number.isFinite(pct)) return '—'
  return `${pct.toFixed(2)}%`
}

export type SuccessRateLevel =
  | 'excellent'
  | 'warning'
  | 'degraded'
  | 'critical'
  | 'unknown'

const SUCCESS_RATE_EXCELLENT_MIN = 99.5
const SUCCESS_RATE_WARNING_MIN = 99
const SUCCESS_RATE_DEGRADED_MIN = 95

/**
 * Single source of truth for grading a success rate (0-100).
 * - excellent: >= 99.5% (green)
 * - warning: >= 99% (yellow)
 * - degraded: >= 95% (orange)
 * - critical: below 95% (red)
 * - unknown: non-finite values
 */
export function getSuccessRateLevel(rate: number): SuccessRateLevel {
  if (!Number.isFinite(rate)) return 'unknown'
  if (rate >= SUCCESS_RATE_EXCELLENT_MIN) return 'excellent'
  if (rate >= SUCCESS_RATE_WARNING_MIN) return 'warning'
  if (rate >= SUCCESS_RATE_DEGRADED_MIN) return 'degraded'
  return 'critical'
}

const SUCCESS_RATE_TEXT_CLASS: Record<SuccessRateLevel, string> = {
  excellent: 'text-emerald-600 dark:text-emerald-400',
  warning: 'text-yellow-600 dark:text-yellow-400',
  degraded: 'text-orange-600 dark:text-orange-400',
  critical: 'text-red-600 dark:text-red-400',
  unknown: 'text-muted-foreground',
}

const SUCCESS_RATE_DOT_CLASS: Record<SuccessRateLevel, string> = {
  excellent: 'bg-emerald-500',
  warning: 'bg-yellow-500',
  degraded: 'bg-orange-500',
  critical: 'bg-red-500',
  unknown: 'bg-muted-foreground',
}

// Hex colors for non-CSS contexts (e.g. chart libraries that need raw values).
const SUCCESS_RATE_HEX_COLOR: Record<SuccessRateLevel, string> = {
  excellent: '#10b981', // emerald-500 (full green)
  warning: '#eab308', // yellow-500
  degraded: '#f97316', // orange-500
  critical: '#ef4444', // red-500
  unknown: '#9ca3af', // gray-400
}

export function getSuccessRateTextClass(rate: number): string {
  return SUCCESS_RATE_TEXT_CLASS[getSuccessRateLevel(rate)]
}

export function getSuccessRateDotClass(rate: number): string {
  return SUCCESS_RATE_DOT_CLASS[getSuccessRateLevel(rate)]
}

export function getSuccessRateColor(rate: number): string {
  return SUCCESS_RATE_HEX_COLOR[getSuccessRateLevel(rate)]
}
