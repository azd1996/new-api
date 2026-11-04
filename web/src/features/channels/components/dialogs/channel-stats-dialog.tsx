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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { getChannelStats } from '../../api'
import type { ProxyModelRoutingStat } from '../../types'

type ChannelStatsDialogProps = {
  channelId: number
  channelName: string
  onClose: () => void
}

// Mounted only while this channel's statistics dialog is open; unmount aborts the query.
export function ChannelStatsDialog(props: ChannelStatsDialogProps) {
  const { t, i18n } = useTranslation()
  const query = useQuery({
    queryKey: ['channels', props.channelId, 'stats'],
    queryFn: ({ signal }) => getChannelStats(props.channelId, signal),
    refetchInterval: 10000,
    refetchOnWindowFocus: false,
    retry: false,
    gcTime: 0,
    meta: { errorToast: false },
  })

  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const percent = new Intl.NumberFormat(locale, {
    style: 'percent',
    maximumFractionDigits: 1,
  })
  const integer = new Intl.NumberFormat(locale, {
    maximumFractionDigits: 0,
  })

  const stats = query.data
  const routing = stats?.proxy_routing ?? []

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Channel statistics')}
      description={props.channelName}
      contentClassName='sm:max-w-3xl'
      bodyClassName='space-y-5'
      footer={
        <Button variant='outline' onClick={props.onClose}>
          {t('Close')}
        </Button>
      }
    >
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          variant='outline'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh')}
        </Button>
      </div>
      {query.isPending && <LoadingState />}
      {query.isError && (
        <ErrorState
          title={t('Failed to load channel statistics')}
          description={getServerErrorMessage(query.error)}
          onRetry={() => void query.refetch()}
        />
      )}
      {stats && (
        <section className='space-y-3' aria-label={t('Proxy routing')}>
          <h3 className='font-medium'>{t('Proxy routing')}</h3>
          {!stats.proxy_ttft_routing_enabled && (
            <p className='text-muted-foreground text-sm'>
              {t(
                'TTFT-aware routing is disabled for this channel; probabilities reflect strict priority (the first healthy proxy).'
              )}
            </p>
          )}
          {routing.length === 0 ? (
            <EmptyState bordered title={t('No proxy routing data yet')} />
          ) : (
            <>
              {routing.map((model) => (
                <ProxyRoutingModelBlock
                  key={model.model}
                  model={model}
                  percent={percent}
                  integer={integer}
                />
              ))}
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Probability is the live per-node selection chance; picks are cumulative per-node selections. Stats reset on restart and are per cluster node.'
                )}
              </p>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Connect latency is per-node and model-independent; forward proxies are weighted by Connect P90 (*), while reverse (tp) proxies are excluded from connect comparison and pinned to tier 1.'
                )}
              </p>
            </>
          )}
        </section>
      )}
    </Dialog>
  )
}

type ProxyRoutingModelBlockProps = {
  model: ProxyModelRoutingStat
  percent: Intl.NumberFormat
  integer: Intl.NumberFormat
}

function ProxyRoutingModelBlock(props: ProxyRoutingModelBlockProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2 rounded-md border p-3'>
      <div className='flex items-center gap-2'>
        <span className='font-medium break-all'>{props.model.model}</span>
        <Badge variant={props.model.gate_active ? 'default' : 'outline'}>
          {props.model.gate_active ? t('Diverting') : t('Strict')}
        </Badge>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Proxy')}</TableHead>
            <TableHead>{t('Probability')}</TableHead>
            <TableHead>{t('Picks')}</TableHead>
            <TableHead>{t('Samples')}</TableHead>
            <TableHead>{t('Avg TTFT')}</TableHead>
            <TableHead>{t('Prompt tokens/s')}</TableHead>
            <TableHead>{t('Connect P50')}</TableHead>
            <TableHead>{`${t('Connect P90')} *`}</TableHead>
            <TableHead>{t('Connect P99')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.model.aliases.map((alias) => (
            <TableRow
              key={alias.alias}
              className={alias.eligible ? undefined : 'text-muted-foreground'}
            >
              <TableCell className='break-all'>
                {alias.eligible
                  ? alias.alias
                  : `${alias.alias} (${t('cooling')})`}
                {alias.is_tp && ` (${t('tp')})`}
              </TableCell>
              <TableCell>{props.percent.format(alias.probability)}</TableCell>
              <TableCell>{props.integer.format(alias.picks)}</TableCell>
              <TableCell>{props.integer.format(alias.samples)}</TableCell>
              <TableCell>
                {alias.samples === 0
                  ? '-'
                  : `${Math.round(alias.avg_ttft_ms)} ms`}
              </TableCell>
              <TableCell>
                {alias.token_speed_samples === 0
                  ? '-'
                  : t('{{value}} tokens/s', {
                      value: props.integer.format(alias.avg_token_speed),
                    })}
              </TableCell>
              <TableCell>
                {alias.connect_p50_ms > 0
                  ? `${Math.round(alias.connect_p50_ms)} ms`
                  : '-'}
              </TableCell>
              <TableCell>
                {alias.connect_p90_ms > 0
                  ? `${Math.round(alias.connect_p90_ms)} ms`
                  : '-'}
              </TableCell>
              <TableCell>
                {alias.connect_p99_ms > 0
                  ? `${Math.round(alias.connect_p99_ms)} ms`
                  : '-'}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
