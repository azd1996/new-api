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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTimeStr, formatTokens, formatUseTime } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'

import { getSessionCacheAnalysis } from '../api'
import {
  SKELETON_REASON_LABELS,
  SKELETON_REASON_VARIANTS,
  buildTurnTimeline,
  skeletonLosses,
  skeletonRate,
  skeletonTierLabels,
  skeletonUpstreamSplit,
} from '../lib/cache-skeleton'
import type { SkeletonLane, SkeletonRates, SkeletonTurn } from '../types'
import { CacheSkeletonDialog } from './dialogs/cache-skeleton-dialog'

const SEGMENT_CLASSES: Record<string, string> = {
  same: 'bg-muted-foreground/30',
  changed: 'bg-destructive/70',
  new: 'bg-success/70',
}

function RatesLine(props: {
  rates: SkeletonRates
  turns: SkeletonTurn[]
  provider: string
}) {
  const { t } = useTranslation()
  const rates = props.rates
  const losses = skeletonLosses(rates)
  const upstream = skeletonUpstreamSplit(props.turns)
  const labels = skeletonTierLabels(props.provider)
  return (
    <div className='flex flex-wrap items-center gap-x-4 gap-y-1'>
      <span>
        {t('Actual hit rate')} <b>{skeletonRate(rates.actual, rates.total)}</b>
      </span>
      <span>
        {t(labels.current)} <b>{skeletonRate(rates.current, rates.total)}</b>
      </span>
      <span>
        {t(labels.adjusted)} <b>{skeletonRate(rates.adjusted, rates.total)}</b>
      </span>
      <span
        title={
          props.provider === 'openai'
            ? t('Azure OpenAI does not support prompt_cache_retention')
            : undefined
        }
      >
        {t(labels.adjusted1h)}{' '}
        <b>{skeletonRate(rates.adjusted_1h, rates.total)}</b>
      </span>
      <span className='text-muted-foreground'>
        {t('Gateway channel switch')} {formatTokens(upstream.channelSwitch)} ·{' '}
        {t('Upstream miss')} {formatTokens(upstream.upstreamMiss)} ·{' '}
        {t(labels.adjustedLoss)} {formatTokens(losses.adjusted)} ·{' '}
        {t('TTL expired')} {formatTokens(losses.ttl)}
      </span>
    </div>
  )
}

function LaneTurns(props: {
  lane: SkeletonLane
  onOpenSkeleton: (requestId: string) => void
}) {
  const { t } = useTranslation()
  const scaleBytes = Math.max(0, ...props.lane.turns.map((x) => x.total_bytes))
  return (
    <Table className='font-mono text-xs'>
      <TableHeader>
        <TableRow>
          <TableHead className='h-7'>{t('Time')}</TableHead>
          <TableHead className='h-7'>{t('Gap')}</TableHead>
          <TableHead className='h-7 w-[40%]'>{t('Prompt prefix')}</TableHead>
          <TableHead className='h-7 text-right'>{t('Actual')}</TableHead>
          <TableHead className='h-7 text-right'>{t('Expected')}</TableHead>
          <TableHead className='h-7'>{t('Channel / key')}</TableHead>
          <TableHead className='h-7'>{t('Reason')}</TableHead>
          <TableHead className='h-7' />
        </TableRow>
      </TableHeader>
      <TableBody className='[&>tr]:h-auto'>
        {props.lane.turns.map((turn) => {
          const bar = buildTurnTimeline(turn, scaleBytes)
          return (
            <TableRow key={`${turn.request_id}-${turn.attempt}`}>
              <TableCell className='py-1'>
                {formatTimeStr(new Date(turn.ts))}
              </TableCell>
              <TableCell className='py-1'>
                {turn.gap_s > 0 ? `+${formatUseTime(turn.gap_s)}` : '-'}
              </TableCell>
              <TableCell className='py-1'>
                <div className='bg-muted relative flex h-3 w-full overflow-hidden rounded-sm'>
                  {bar.segments.map((segment) => (
                    <div
                      key={segment.state}
                      className={SEGMENT_CLASSES[segment.state]}
                      style={{ width: `${segment.widthPct}%` }}
                    />
                  ))}
                  {bar.breakpoints.map((bp) => (
                    <span
                      key={bp.leftPct}
                      aria-hidden='true'
                      className='bg-primary absolute top-0 h-full w-0.5'
                      style={{ left: `calc(${bp.leftPct}% - 1px)` }}
                      title={`◆ ${bp.ttl}`}
                    />
                  ))}
                </div>
              </TableCell>
              <TableCell className='py-1 text-right'>
                {skeletonRate(turn.actual_read, turn.total_tokens)}
              </TableCell>
              <TableCell className='py-1 text-right'>
                {skeletonRate(turn.expected_current, turn.total_tokens)}
                {turn.expected_adjusted > turn.expected_current &&
                  ` → ${skeletonRate(turn.expected_adjusted, turn.total_tokens)}`}
              </TableCell>
              <TableCell className='text-muted-foreground py-1'>
                {turn.channel_id} / {turn.key_idx}
              </TableCell>
              <TableCell className='space-x-1 py-1'>
                <StatusBadge
                  size='sm'
                  variant={SKELETON_REASON_VARIANTS[turn.reason] ?? 'neutral'}
                  label={t(SKELETON_REASON_LABELS[turn.reason] ?? turn.reason)}
                />
                {turn.tail_volatile && (
                  <span className='text-warning'>
                    ⚠ {t('Volatile tail breakpoint')}
                  </span>
                )}
                {turn.cache_key_changed && (
                  <span className='text-warning'>
                    ⚠ {t('prompt_cache_key changed')}
                  </span>
                )}
                {turn.no_cache_key && (
                  <span className='text-muted-foreground'>
                    {t('No prompt_cache_key')}
                  </span>
                )}
                {turn.diverge_idx >= 0 && (
                  <span className='text-destructive'>
                    ✗ #{turn.diverge_idx}
                  </span>
                )}
              </TableCell>
              <TableCell className='py-1 text-right'>
                <Button
                  variant='ghost'
                  size='sm'
                  className='h-6 px-2 text-xs'
                  onClick={() => props.onOpenSkeleton(turn.request_id)}
                >
                  {t('Skeleton')}
                </Button>
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

// SessionCacheDiagnosis replays the session's request skeletons through the
// prompt-cache simulation: actual hit rate against the ceilings with the
// client's breakpoints, with the gateway adjustment and with a 1h TTL.
export function SessionCacheDiagnosis(props: { sessionId: string }) {
  const { t } = useTranslation()
  const [showTurns, setShowTurns] = useState(false)
  const [skeletonRequestId, setSkeletonRequestId] = useState('')
  const query = useQuery({
    queryKey: ['usage-logs-session-cache', props.sessionId],
    queryFn: async () =>
      requireServerSuccess(await getSessionCacheAnalysis(props.sessionId)).data,
    meta: { errorToast: false },
    retry: false,
  })
  const data = query.data
  const lanes = data?.lanes ?? []
  if (!data?.enabled || !data.summary || lanes.length === 0) return null

  return (
    <section
      aria-label={t('Cache diagnosis')}
      className='space-y-2 border-b px-3 py-2 text-xs'
    >
      <div className='flex items-center gap-3'>
        <span className='font-semibold'>{t('Cache diagnosis')}</span>
        <RatesLine
          rates={data.summary}
          turns={lanes.flatMap((l) => l.turns)}
          provider={lanes[0].provider}
        />
        <Button
          variant='ghost'
          size='sm'
          className='ml-auto h-6 px-2 text-xs'
          aria-expanded={showTurns}
          onClick={() => setShowTurns((v) => !v)}
        >
          {showTurns ? t('Hide turns') : t('Show turns')}
        </Button>
      </div>
      {data.truncated && (
        <div className='text-warning'>
          {t('Only the first 2000 attempts of this session are analyzed')}
        </div>
      )}
      {showTurns &&
        lanes.map((lane) => (
          <div key={lane.lane} className='space-y-1'>
            {lanes.length > 1 && (
              <div className='text-muted-foreground flex flex-wrap gap-x-3'>
                <span className='font-mono'>
                  lane {lane.lane.slice(0, 8)} · {lane.model} ·{' '}
                  {lane.turns.length} {t('turns')}
                </span>
                <RatesLine
                  rates={lane.summary}
                  turns={lane.turns}
                  provider={lane.provider}
                />
              </div>
            )}
            <LaneTurns lane={lane} onOpenSkeleton={setSkeletonRequestId} />
          </div>
        ))}
      <CacheSkeletonDialog
        requestId={skeletonRequestId}
        open={!!skeletonRequestId}
        onOpenChange={(open) => !open && setSkeletonRequestId('')}
      />
    </section>
  )
}
