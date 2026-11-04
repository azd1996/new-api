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
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getCacheSkeleton } from '../../api'
import { buildSkeletonGroups, formatBytes } from '../../lib/cache-skeleton'
import type { CacheSkeleton } from '../../types'

interface CacheSkeletonDialogProps {
  requestId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

function SkeletonAttempt(props: { item: CacheSkeleton; showAttempt: boolean }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const item = props.item
  const totalInput = item.cache_read + item.cache_creation + item.input
  const hitRate =
    totalInput > 0
      ? `${((item.cache_read / totalInput) * 100).toFixed(1)}%`
      : '—'
  const groups = buildSkeletonGroups(item)
  const fixedLabels: Record<string, string> = {
    added: t('Added'),
    moved: t('Moved'),
    pass_through: t('Skipped: pass-through mode'),
  }
  const fixedLabel = item.fixed ? (fixedLabels[item.fixed] ?? item.fixed) : '-'

  return (
    <section className='space-y-2'>
      <div className='bg-muted/40 space-y-1 rounded-md p-3 font-mono text-xs'>
        {props.showAttempt && (
          <div className='font-semibold'>
            {t('Attempt')} {item.attempt + 1} · {item.status}
          </div>
        )}
        <div>
          {item.model} · thinking {item.thinking || '-'} · effort{' '}
          {item.effort || '-'} · {t('Param fingerprint')}{' '}
          {item.param_fp.slice(0, 8)}
        </div>
        <div>
          {t('Session ID')} {item.session_id || '-'} · lane{' '}
          {item.lane.slice(0, 8)} · {t('Channel')} {item.channel_id} / key{' '}
          {item.key_idx} · {item.node}
        </div>
        {item.provider === 'openai' ? (
          <div>
            prompt_cache_key {item.cache_key_hash?.slice(0, 8) || '-'} ·
            prompt_cache_retention {item.retention || '-'}
            {item.parent_request_id && (
              <> · previous_response_id {item.parent_request_id}</>
            )}
          </div>
        ) : (
          <div>
            {t('Breakpoints')} {item.bp_idx.length} (
            {item.bp_ttl.join(', ') || '-'}) · {t('Gateway rewrite')}{' '}
            {fixedLabel}
          </div>
        )}
        <div>
          {t('Cache read')} {formatNumber(item.cache_read, locale)} ·{' '}
          {t('Cache write')} {formatNumber(item.cache_creation, locale)} ·{' '}
          {t('Uncached input')} {formatNumber(item.input, locale)} ·{' '}
          {t('Hit rate')} {hitRate}
        </div>
      </div>

      <Table className='min-w-[560px] font-mono text-xs'>
        <TableHeader>
          <TableRow>
            <TableHead className='h-7'>#</TableHead>
            <TableHead className='h-7'>{t('Block type')}</TableHead>
            <TableHead className='h-7'>{t('Name')}</TableHead>
            <TableHead className='h-7'>{t('Hash')}</TableHead>
            <TableHead className='h-7 text-right'>{t('Size')}</TableHead>
            <TableHead className='h-7 text-right'>{t('Cumulative')}</TableHead>
            <TableHead className='h-7'>{t('Breakpoint')}</TableHead>
          </TableRow>
        </TableHeader>
        {groups.map((group) => (
          <TableBody key={group.key} className='[&>tr]:h-auto'>
            <TableRow className='hover:bg-transparent'>
              <TableCell colSpan={7} className='pt-2 pb-1 font-semibold'>
                {group.messageIndex === undefined
                  ? `${group.label} (${group.rows.length})`
                  : `m${group.messageIndex} ${group.label}`}
              </TableCell>
            </TableRow>
            {group.rows.map((row) => (
              <TableRow
                key={row.index}
                className={cn(
                  row.breakpointTtl !== undefined && 'bg-primary/5',
                  row.tailSystemBreakpoint && 'bg-amber-500/10'
                )}
              >
                <TableCell className='py-0.5'>{row.index}</TableCell>
                <TableCell className='py-0.5'>{row.type}</TableCell>
                <TableCell className='py-0.5'>{row.name}</TableCell>
                <TableCell className='text-muted-foreground py-0.5'>
                  {row.hash}
                </TableCell>
                <TableCell className='py-0.5 text-right'>
                  {formatBytes(row.bytes)}
                </TableCell>
                <TableCell className='py-0.5 text-right'>
                  {formatBytes(row.cumulativeBytes)}
                </TableCell>
                <TableCell className='py-0.5'>
                  {row.breakpointTtl !== undefined && (
                    <span>◆ {row.breakpointTtl}</span>
                  )}
                  {row.tailSystemBreakpoint && (
                    <span className='ml-1 text-amber-600'>
                      ⚠ {t('Breakpoint on the last system message')}
                    </span>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        ))}
      </Table>
    </section>
  )
}

// CacheSkeletonDialog shows the recorded prompt-cache skeleton of one request:
// block hashes, kinds and sizes only, never prompt text.
export function CacheSkeletonDialog(props: CacheSkeletonDialogProps) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['usage-logs-cache-skeleton', props.requestId],
    queryFn: async () =>
      requireServerSuccess(await getCacheSkeleton(props.requestId)).data,
    enabled: props.open && !!props.requestId,
    staleTime: 0,
    // A failure here is a deterministic server error; the global retry policy
    // would hold the spinner for ~15s before showing it.
    retry: false,
  })
  const items = query.data?.items ?? []

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Request Skeleton')}
      description={t(
        'Prompt-cache structure of the outbound request: block hashes and sizes only, no prompt text'
      )}
      contentClassName='sm:max-w-4xl'
      contentHeight='min(80dvh, 820px)'
      bodyClassName='pr-2 sm:pr-4'
    >
      {query.isLoading && <LoadingState />}
      {query.isError && <ErrorState onRetry={() => query.refetch()} />}
      {query.data && !query.data.enabled && (
        <EmptyState
          title={t('Request skeleton is not recorded')}
          description={t(
            'Request skeletons are recorded only when the log database is ClickHouse'
          )}
        />
      )}
      {query.data?.enabled && items.length === 0 && (
        <EmptyState
          title={t('No request skeleton for this request')}
          description={t(
            'Only Claude Messages, OpenAI Chat Completions and Responses requests are recorded; rows expire after the retention period'
          )}
        />
      )}
      <div className='space-y-6'>
        {items.map((item) => (
          <SkeletonAttempt
            key={`${item.attempt}-${item.ts}`}
            item={item}
            showAttempt={items.length > 1}
          />
        ))}
      </div>
    </Dialog>
  )
}
