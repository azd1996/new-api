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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { BarChart3, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { getLogsAnalysis } from '../api'
import { buildApiParams } from '../lib/utils'
import type {
  CacheMinTokenRule,
  LogAnalysisChannel,
  LogCacheCell,
  LogCacheTable,
  LogLatencyCell,
  LogLatencyTable,
  LogSuccessCell,
  LogSuccessTable,
} from '../types'
import { useLogsViewScope } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const DASH = '—'

// formatSeconds renders a millisecond duration as seconds: whole seconds once
// the value is >= 10s (matching the compact look of the reference table), one
// decimal below that, and a dash for empty/zero samples.
function formatSeconds(ms: number): string {
  if (!ms || ms <= 0) return DASH
  const sec = ms / 1000
  return sec >= 10 ? `${Math.round(sec)}s` : `${sec.toFixed(1)}s`
}

// latencyCellText builds the "latency / FRT / count" string for one cell.
function latencyCellText(cell: LogLatencyCell | undefined): string {
  if (!cell || cell.cnt === 0) return DASH
  return `${formatSeconds(cell.lat)} / ${formatSeconds(cell.frt)} / ${cell.cnt}`
}

// formatRate renders "pct% / cache_read / denominator"; dash when no eligible
// prompt tokens exist for that denominator.
function formatRate(rate: number, read: number, denominator: number): string {
  if (!denominator || denominator <= 0) return DASH
  return `${(rate * 100).toFixed(1)}% / ${read} / ${denominator}`
}

// formatPercent renders a 0-1 ratio as a percentage; dash when total is 0.
function formatPercent(rate: number, total: number): string {
  if (!total || total <= 0) return DASH
  return `${(rate * 100).toFixed(1)}%`
}

function LatencyStatTable(props: {
  title: string
  table: LogLatencyTable
  channels: LogAnalysisChannel[]
  summaryLabel: string
  bucketLabel: string
}) {
  return (
    <div className='space-y-2'>
      <h3 className='text-sm font-medium'>{props.title}</h3>
      <ScrollArea className='rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='whitespace-nowrap'>
                {props.bucketLabel}
              </TableHead>
              <TableHead className='whitespace-nowrap font-semibold'>
                {props.summaryLabel}
              </TableHead>
              {props.channels.map((channel) => (
                <TableHead
                  key={channel.id}
                  className='whitespace-nowrap'
                  title={`#${channel.id}`}
                >
                  {channel.name}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.table.buckets.map((row) => (
              <TableRow key={row.bucket}>
                <TableCell className='whitespace-nowrap font-medium'>
                  {row.bucket}
                </TableCell>
                <TableCell className='whitespace-nowrap font-semibold'>
                  {latencyCellText(row.total)}
                </TableCell>
                {props.channels.map((channel, index) => (
                  <TableCell key={channel.id} className='whitespace-nowrap'>
                    {latencyCellText(row.channels[index])}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <ScrollBar orientation='horizontal' />
      </ScrollArea>
    </div>
  )
}

function CacheStatTable(props: {
  title: string
  cache: LogCacheTable
  channels: LogAnalysisChannel[]
  summaryLabel: string
  metricLabel: string
  rows: { key: string; label: string; value: (cell: LogCacheCell) => string }[]
}) {
  return (
    <div className='space-y-2'>
      <h3 className='text-sm font-medium'>{props.title}</h3>
      <ScrollArea className='rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='whitespace-nowrap'>
                {props.metricLabel}
              </TableHead>
              <TableHead className='whitespace-nowrap font-semibold'>
                {props.summaryLabel}
              </TableHead>
              {props.channels.map((channel) => (
                <TableHead
                  key={channel.id}
                  className='whitespace-nowrap'
                  title={`#${channel.id}`}
                >
                  {channel.name}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.rows.map((row) => (
              <TableRow key={row.key}>
                <TableCell className='whitespace-nowrap font-medium'>
                  {row.label}
                </TableCell>
                <TableCell className='whitespace-nowrap font-semibold'>
                  {row.value(props.cache.total)}
                </TableCell>
                {props.channels.map((channel, index) => {
                  const cell = props.cache.channels[index]
                  return (
                    <TableCell key={channel.id} className='whitespace-nowrap'>
                      {cell ? row.value(cell) : DASH}
                    </TableCell>
                  )
                })}
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <ScrollBar orientation='horizontal' />
      </ScrollArea>
    </div>
  )
}

function ThresholdsTable(props: {
  title: string
  thresholds: CacheMinTokenRule[]
  defaultTokens: number
  labels: { model: string; series: string; minTokens: string; fallback: string }
}) {
  return (
    <div className='space-y-2'>
      <h3 className='text-sm font-medium'>{props.title}</h3>
      <ScrollArea className='rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='whitespace-nowrap'>
                {props.labels.model}
              </TableHead>
              <TableHead className='whitespace-nowrap'>
                {props.labels.series}
              </TableHead>
              <TableHead className='whitespace-nowrap'>
                {props.labels.minTokens}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.thresholds.map((rule) => (
              <TableRow key={`${rule.series}-${rule.label}`}>
                <TableCell className='whitespace-nowrap font-medium'>
                  {rule.label}
                </TableCell>
                <TableCell className='whitespace-nowrap uppercase'>
                  {rule.series}
                </TableCell>
                <TableCell className='whitespace-nowrap'>
                  {rule.min_tokens}
                </TableCell>
              </TableRow>
            ))}
            <TableRow>
              <TableCell className='text-muted-foreground whitespace-nowrap'>
                {props.labels.fallback}
              </TableCell>
              <TableCell className='text-muted-foreground'>—</TableCell>
              <TableCell className='text-muted-foreground whitespace-nowrap'>
                {props.defaultTokens || DASH}
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
        <ScrollBar orientation='horizontal' />
      </ScrollArea>
    </div>
  )
}

function SuccessStatTable(props: {
  title: string
  table: LogSuccessTable
  channels: LogAnalysisChannel[]
  summaryLabel: string
  metricLabel: string
  rows: { key: string; label: string; value: (cell: LogSuccessCell) => string }[]
}) {
  return (
    <div className='space-y-2'>
      <h3 className='text-sm font-medium'>{props.title}</h3>
      <ScrollArea className='rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='whitespace-nowrap'>
                {props.metricLabel}
              </TableHead>
              <TableHead className='whitespace-nowrap font-semibold'>
                {props.summaryLabel}
              </TableHead>
              {props.channels.map((channel) => (
                <TableHead
                  key={channel.id}
                  className='whitespace-nowrap'
                  title={`#${channel.id}`}
                >
                  {channel.name}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.rows.map((row) => (
              <TableRow key={row.key}>
                <TableCell className='whitespace-nowrap font-medium'>
                  {row.label}
                </TableCell>
                <TableCell className='whitespace-nowrap font-semibold'>
                  {row.value(props.table.total)}
                </TableCell>
                {props.channels.map((channel, index) => {
                  const cell = props.table.channels[index]
                  return (
                    <TableCell key={channel.id} className='whitespace-nowrap'>
                      {cell ? row.value(cell) : DASH}
                    </TableCell>
                  )
                })}
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <ScrollBar orientation='horizontal' />
      </ScrollArea>
    </div>
  )
}

// LogAnalysisDialog is the admin-only analysis button on the common log list: it
// runs a prompt-size latency/FRT + cache-hit + success-rate analysis over exactly
// the filtered logs (same filters the list uses). Renders nothing for non-admin.
export function LogAnalysisDialog() {
  const { t } = useTranslation()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const searchParams = route.useSearch()
  const [open, setOpen] = useState(false)

  const query = useQuery({
    queryKey: ['usage-logs-analysis', searchParams],
    queryFn: () =>
      getLogsAnalysis(
        buildApiParams({ page: 1, pageSize: 1, searchParams, isAdmin: true })
      ),
    enabled: open && isAdmin,
    staleTime: 0,
  })

  if (!isAdmin) return null

  const result = query.data?.data
  const summaryLabel = t('Summary')

  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title={t('Log Analysis')}
      description={t(
        'Prompt-size latency & cache-hit analysis of the filtered logs'
      )}
      contentClassName='w-[97vw] max-w-[97vw] sm:max-w-[97vw]'
      trigger={
        <Button variant='outline' size='sm' className='gap-1.5'>
          <BarChart3 className='size-4' />
          {t('Analysis')}
        </Button>
      }
    >
      <div className='space-y-5'>
        {/* PLACEHOLDER_BODY */}
        {query.isLoading && (
          <div className='text-muted-foreground flex items-center justify-center gap-2 py-12 text-sm'>
            <Loader2 className='size-4 animate-spin' />
            {t('Loading')}
          </div>
        )}
        {query.isError && (
          <div className='text-destructive py-12 text-center text-sm'>
            {t('Failed to load analysis')}
          </div>
        )}
        {result && !query.isLoading && result.scanned === 0 && (
          <div className='text-muted-foreground py-12 text-center text-sm'>
            {t('No data')}
          </div>
        )}
        {result && !query.isLoading && result.scanned > 0 && (
          <>
            <p className='text-muted-foreground text-xs'>
              {t('Each cell: latency / FRT / count')}
              {' · '}
              {t('Scanned {{count}} logs', { count: result.scanned })}
              {result.truncated
                ? ` · ${t('partial sample (scan limit reached)')}`
                : ''}
            </p>
            <LatencyStatTable
              title={t('Mean')}
              table={result.latency.mean}
              channels={result.channels}
              summaryLabel={summaryLabel}
              bucketLabel={t('Prompt Size')}
            />
            <LatencyStatTable
              title={t('P50 (median)')}
              table={result.latency.p50}
              channels={result.channels}
              summaryLabel={summaryLabel}
              bucketLabel={t('Prompt Size')}
            />
            <LatencyStatTable
              title={t('P90')}
              table={result.latency.p90}
              channels={result.channels}
              summaryLabel={summaryLabel}
              bucketLabel={t('Prompt Size')}
            />
            <LatencyStatTable
              title={t('P99')}
              table={result.latency.p99}
              channels={result.channels}
              summaryLabel={summaryLabel}
              bucketLabel={t('Prompt Size')}
            />
            {/* PLACEHOLDER_BODY2 */}
            <SuccessStatTable
              title={t('Success Rate')}
              table={result.success}
              channels={result.channels}
              summaryLabel={summaryLabel}
              metricLabel={t('Metric')}
              rows={[
                {
                  key: 'rate',
                  label: t('Success rate'),
                  value: (cell) => formatPercent(cell.rate, cell.total),
                },
                {
                  key: 'total',
                  label: t('Total requests'),
                  value: (cell) => String(cell.total),
                },
                {
                  key: 'success',
                  label: t('Success (consume)'),
                  value: (cell) => String(cell.success),
                },
                {
                  key: 'error',
                  label: t('Error'),
                  value: (cell) => String(cell.error),
                },
                {
                  key: 'retry_override',
                  label: t('Retry override'),
                  value: (cell) => String(cell.retry_override),
                },
              ]}
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Total requests ≈ consume + error - retry-override (retried intermediate attempts); success = consume count. Success rate = success / total.'
              )}
            </p>
            {/* PLACEHOLDER_BODY3 */}
            <CacheStatTable
              title={t('Cache Hit Rate')}
              cache={result.cache}
              channels={result.channels}
              summaryLabel={summaryLabel}
              metricLabel={t('Metric')}
              rows={[
                {
                  key: 'gross',
                  label: t('Gross hit rate'),
                  value: (cell) =>
                    formatRate(cell.gross_rate, cell.cache_read, cell.input_all),
                },
                {
                  key: 'eligible',
                  label: t('Eligible hit rate'),
                  value: (cell) =>
                    formatRate(
                      cell.eligible_rate,
                      cell.cache_read,
                      cell.input_eligible
                    ),
                },
                {
                  key: 'hit',
                  label: t('Hit'),
                  value: (cell) => String(cell.hit),
                },
                {
                  key: 'cold_write',
                  label: t('Cold write'),
                  value: (cell) => String(cell.cold_write),
                },
                {
                  key: 'too_small',
                  label: t('Too small to cache'),
                  value: (cell) => String(cell.too_small),
                },
                {
                  key: 'other_miss',
                  label: t('Other miss'),
                  value: (cell) => String(cell.other_miss),
                },
              ]}
            />
            {/* PLACEHOLDER_BODY4 */}
            <p className='text-muted-foreground text-xs'>
              {t(
                'Rate = cache_read / total input tokens (for Claude, prompt_tokens excludes cache, so total = prompt + cache_read + cache_creation; OpenAI already includes cached tokens in prompt). Eligible excludes only too-small requests; cold writes count as misses. OpenAI cold writes cannot be detected and fall into "other miss".'
              )}
            </p>
            <ThresholdsTable
              title={t('Cache minimum token thresholds (by model)')}
              thresholds={result.thresholds}
              defaultTokens={result.threshold_default}
              labels={{
                model: t('Model'),
                series: t('Series'),
                minTokens: t('Min cacheable tokens'),
                fallback: t('Default (other models)'),
              }}
            />
          </>
        )}
      </div>
    </Dialog>
  )
}
