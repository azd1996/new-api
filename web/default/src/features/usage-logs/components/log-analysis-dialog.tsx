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
import { toPng } from 'html-to-image'
import { BarChart3, FileDown, ImageDown, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
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
  LogAnalysisResult,
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

// ExportSection is one titled table flattened to plain strings, shared by both
// the Markdown and image exporters so cell formatting has a single source.
interface ExportSection {
  title: string
  headers: string[]
  rows: string[][]
}

function buildAnalysisSections(
  result: LogAnalysisResult,
  t: (key: string) => string,
  summaryLabel: string
): ExportSection[] {
  const channelNames = result.channels.map((c) => c.name)
  const latencyHeaders = [t('Prompt Size'), summaryLabel, ...channelNames]
  const latencySection = (
    title: string,
    table: LogLatencyTable
  ): ExportSection => ({
    title,
    headers: latencyHeaders,
    rows: table.buckets.map((row) => [
      row.bucket,
      latencyCellText(row.total),
      ...row.channels.map((c) => latencyCellText(c)),
    ]),
  })

  const metricHeaders = [t('Metric'), summaryLabel, ...channelNames]
  const successRow = (
    label: string,
    accessor: (cell: LogSuccessCell) => string
  ): string[] => [
    label,
    accessor(result.success.total),
    ...result.success.channels.map(accessor),
  ]
  const cacheRow = (
    label: string,
    accessor: (cell: LogCacheCell) => string
  ): string[] => [
    label,
    accessor(result.cache.total),
    ...result.cache.channels.map(accessor),
  ]

  return [
    latencySection(t('Mean'), result.latency.mean),
    latencySection(t('P50 (median)'), result.latency.p50),
    latencySection(t('P90'), result.latency.p90),
    latencySection(t('P99'), result.latency.p99),
    {
      title: t('Success Rate'),
      headers: metricHeaders,
      rows: [
        successRow(t('Success rate'), (c) => formatPercent(c.rate, c.total)),
        successRow(t('Total requests'), (c) => String(c.total)),
        successRow(t('Success (consume)'), (c) => String(c.success)),
        successRow(t('Error'), (c) => String(c.error)),
        successRow(t('Retry override'), (c) => String(c.retry_override)),
      ],
    },
    {
      title: t('Cache Hit Rate'),
      headers: metricHeaders,
      rows: [
        cacheRow(t('Gross hit rate'), (c) =>
          formatRate(c.gross_rate, c.cache_read, c.input_all)
        ),
        cacheRow(t('Eligible hit rate'), (c) =>
          formatRate(c.eligible_rate, c.cache_read, c.input_eligible)
        ),
        cacheRow(t('Hit'), (c) => String(c.hit)),
        cacheRow(t('Cold write'), (c) => String(c.cold_write)),
        cacheRow(t('Too small to cache'), (c) => String(c.too_small)),
        cacheRow(t('Other miss'), (c) => String(c.other_miss)),
      ],
    },
    {
      title: t('Cache minimum token thresholds (by model)'),
      headers: [t('Model'), t('Series'), t('Min cacheable tokens')],
      rows: [
        ...result.thresholds.map((r) => [
          r.label,
          r.series,
          String(r.min_tokens),
        ]),
        [
          t('Default (other models)'),
          DASH,
          result.threshold_default ? String(result.threshold_default) : DASH,
        ],
      ],
    },
  ]
}

function escapeMarkdownCell(value: string): string {
  return value.replaceAll('|', '\\|')
}

function sectionsToMarkdown(title: string, sections: ExportSection[]): string {
  const blocks = sections.map((s) => {
    const head = `| ${s.headers.map(escapeMarkdownCell).join(' | ')} |`
    const sep = `| ${s.headers.map(() => '---').join(' | ')} |`
    const body = s.rows
      .map((r) => `| ${r.map(escapeMarkdownCell).join(' | ')} |`)
      .join('\n')
    return `### ${s.title}\n\n${head}\n${sep}\n${body}`
  })
  return `# ${title}\n\n${blocks.join('\n\n')}\n`
}

function escapeHtml(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
}

function sectionsToHtml(title: string, sections: ExportSection[]): string {
  const cellBorder = 'border:1px solid #d1d5db;padding:6px 10px;white-space:nowrap;'
  const renderTable = (s: ExportSection): string => {
    const head = s.headers
      .map(
        (h) =>
          `<th style="${cellBorder}background:#f3f4f6;text-align:left;font-weight:600;">${escapeHtml(h)}</th>`
      )
      .join('')
    const bodyRows = s.rows
      .map((r) => {
        const tds = r
          .map(
            (c, i) =>
              `<td style="${cellBorder}${i === 0 ? 'font-weight:500;' : ''}">${escapeHtml(c)}</td>`
          )
          .join('')
        return `<tr>${tds}</tr>`
      })
      .join('')
    return `<h3 style="margin:16px 0 6px;font-size:14px;">${escapeHtml(s.title)}</h3><table style="border-collapse:collapse;font-size:12px;color:#111827;"><thead><tr>${head}</tr></thead><tbody>${bodyRows}</tbody></table>`
  }
  return `<div style="padding:20px;background:#ffffff;color:#111827;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;width:fit-content;"><h2 style="margin:0 0 4px;font-size:16px;">${escapeHtml(title)}</h2>${sections.map(renderTable).join('')}</div>`
}

function exportStamp(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`
}

function triggerDownload(href: string, filename: string): void {
  const a = document.createElement('a')
  a.href = href
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
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

// LogAnalysisDialog is the admin-only "特殊按钮" on the common log list: it runs
// a prompt-size latency/FRT + cache-hit + success-rate analysis over exactly the
// filtered logs (same filters the list uses). Renders nothing for non-admin.
export function LogAnalysisDialog() {
  const { t } = useTranslation()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const searchParams = route.useSearch()
  const [open, setOpen] = useState(false)
  const [exporting, setExporting] = useState(false)

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
  const canExport = !!result && result.scanned > 0

  const handleExportMarkdown = () => {
    if (!result) return
    const sections = buildAnalysisSections(result, t, summaryLabel)
    const md = sectionsToMarkdown(t('Log Analysis'), sections)
    const url = URL.createObjectURL(
      new Blob([md], { type: 'text/markdown;charset=utf-8' })
    )
    triggerDownload(url, `log-analysis-${exportStamp()}.md`)
    URL.revokeObjectURL(url)
  }

  const handleExportImage = async () => {
    if (!result || exporting) return
    setExporting(true)
    const container = document.createElement('div')
    container.style.position = 'fixed'
    container.style.left = '-99999px'
    container.style.top = '0'
    container.innerHTML = sectionsToHtml(
      t('Log Analysis'),
      buildAnalysisSections(result, t, summaryLabel)
    )
    document.body.appendChild(container)
    try {
      const target = container.firstElementChild as HTMLElement
      const dataUrl = await toPng(target, {
        pixelRatio: 2,
        backgroundColor: '#ffffff',
      })
      triggerDownload(dataUrl, `log-analysis-${exportStamp()}.png`)
    } catch {
      toast.error(t('Export failed'))
    } finally {
      document.body.removeChild(container)
      setExporting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          <Button variant='outline' size='sm' className='gap-1.5' />
        }
      >
        <BarChart3 className='size-4' />
        {t('Analysis')}
      </DialogTrigger>
      <DialogContent className='flex max-h-[92vh] w-[97vw] max-w-[97vw] flex-col gap-4 overflow-hidden sm:max-w-[97vw]'>
        <DialogHeader>
          <DialogTitle>{t('Log Analysis')}</DialogTitle>
          <DialogDescription>
            {t('Prompt-size latency & cache-hit analysis of the filtered logs')}
          </DialogDescription>
          <div className='flex flex-wrap gap-2 pt-1'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='gap-1.5'
              disabled={!canExport || exporting}
              onClick={handleExportImage}
            >
              {exporting ? (
                <Loader2 className='size-4 animate-spin' />
              ) : (
                <ImageDown className='size-4' />
              )}
              {t('Export image')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='gap-1.5'
              disabled={!canExport}
              onClick={handleExportMarkdown}
            >
              <FileDown className='size-4' />
              {t('Export Markdown')}
            </Button>
          </div>
        </DialogHeader>

        <div className='min-h-0 flex-1 space-y-5 overflow-y-auto pr-1'>
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
          {result && !query.isLoading && (
            <>
              {result.scanned === 0 && (
                <div className='text-muted-foreground py-12 text-center text-sm'>
                  {t('No data')}
                </div>
              )}
              {result.scanned > 0 && (
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
                          formatRate(
                            cell.gross_rate,
                            cell.cache_read,
                            cell.input_all
                          ),
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
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
