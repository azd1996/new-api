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
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { skeletonRate } from '../lib/cache-skeleton'
import type { SkeletonLossSummary } from '../types'

export function CacheLossBreakdown(props: { summary?: SkeletonLossSummary }) {
  const { t, i18n } = useTranslation()
  const summary = props.summary
  if (!summary || summary.analyzed_turns <= 0) return null

  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const labels: Record<string, string> = {
    first_turn: t('First turn'),
    too_small: t('Below cache minimum'),
    no_breakpoint: t('No cache breakpoint'),
    param_changed: t('Cache-affecting parameters changed'),
    prefix_changed: t('Prefix or message history changed'),
    concurrent: t('Possible concurrent overlap'),
    ttl_expired: t('TTL expired'),
    breakpoint_misplaced: t('Misplaced cache breakpoint'),
    breakpoint_stalled: t('Breakpoint coverage insufficient / not advanced'),
    stable_unexplained: t('Stable history unexplained'),
    lookback_exceeded: t('Cache lookback exceeded'),
    channel_switch: t('Channel switch correlation'),
    upstream_miss: t('Unexplained upstream gap'),
    new_content: t('New content / residual'),
  }
  const items = [...summary.items].sort((a, b) => b.tokens - a.tokens)
  const stalled = summary.items.find(
    (item) => item.reason === 'breakpoint_stalled'
  )

  return (
    <section
      aria-label={t('Cache loss breakdown')}
      className='w-full min-w-0 space-y-2 text-xs'
    >
      <div className='flex flex-wrap gap-x-4 gap-y-1'>
        <span className='font-semibold'>{t('Cache loss breakdown')}</span>
        <span>
          {t('Analyzed attempts')}:{' '}
          {formatNumber(summary.analyzed_turns, locale)}
        </span>
        <span>
          {t('Total input tokens')}:{' '}
          {formatNumber(summary.total_tokens, locale)}
        </span>
        <span>
          {t('Actual cache read tokens')}:{' '}
          {formatNumber(summary.actual_read, locale)}
        </span>
        <span>
          {t('Uncached tokens')}:{' '}
          {formatNumber(summary.uncached_tokens, locale)} (
          {skeletonRate(summary.uncached_tokens, summary.total_tokens)})
        </span>
      </div>
      {stalled && stalled.tokens > 0 && (
        <p className='text-warning'>
          {t(
            'Breakpoints did not cover growing stable history: {{tokens}} estimated tokens across {{turns}} attempts. Advance breakpoints as the conversation grows; tail system breakpoint correction alone does not address this.',
            {
              tokens: formatNumber(stalled.tokens, locale),
              turns: formatNumber(stalled.turns, locale),
            }
          )}
        </p>
      )}
      {items.length > 0 && (
        <StaticDataTable
          tableProps={{ 'aria-label': t('Cache loss breakdown') }}
          tableClassName='[&_td]:py-1 [&_td]:text-xs [&_th]:h-7 [&_th]:text-xs [&_tbody>tr]:h-auto'
          data={items}
          getRowKey={(item) => item.reason}
          columns={[
            {
              id: 'reason',
              header: t('Reason'),
              cell: (item) => labels[item.reason] ?? item.reason,
            },
            {
              id: 'turns',
              header: t('Affected attempts'),
              className: 'text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (item) => formatNumber(item.turns, locale),
            },
            {
              id: 'tokens',
              header: t('Estimated tokens'),
              className: 'text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (item) => formatNumber(item.tokens, locale),
            },
            {
              id: 'share',
              header: t('Share of uncached tokens'),
              className: 'text-right',
              cellClassName: 'text-right tabular-nums',
              cell: (item) =>
                skeletonRate(item.tokens, summary.uncached_tokens),
            },
          ]}
        />
      )}
      <p className='text-muted-foreground'>
        {t(
          'Breakpoint advancement and longer TTL rates are simulations, not guaranteed gains. Stable history unexplained means the remaining gap has no confirmed cause.'
        )}
      </p>
      <p className='text-muted-foreground'>
        {t(
          'Claude only, successful analyzable attempts. Byte-proportional estimates partition uncached input without overlap; attempts may appear in multiple reasons. First turns, changed input and new content are not necessarily recoverable.'
        )}
      </p>
      <p className='text-muted-foreground'>
        {t(
          'Channel switches show correlation, not a confirmed cache-domain change. Concurrent overlap uses a conservative end-time bound. Upstream gaps are unexplained estimates, not proof of upstream fault.'
        )}
      </p>
      <p className='text-muted-foreground'>
        {t(
          'Byte-based approximation, not an exact token reconciliation; byte-to-token ratios vary by language and content type. Do not use these figures as a billing reconciliation source.'
        )}
      </p>
    </section>
  )
}
