import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DatePicker } from '@/components/date-picker'
import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { ByteCacheCoverageSummary } from '@/features/usage-logs/components/cache-coverage-summary'
import { CacheLossBreakdown } from '@/features/usage-logs/components/cache-loss-breakdown'
import dayjs from '@/lib/dayjs'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { getCustomerCacheAnalysis, getCustomerLogAnalysis } from '../api'
import type { Customer } from '../types'
import { CustomerStatistics } from './customer-statistics'

export function CustomerStatsDialog(props: {
  customer: Customer
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [start, setStart] = useState<Date | undefined>(() =>
    dayjs().startOf('day').toDate()
  )
  const [end, setEnd] = useState<Date | undefined>(() => dayjs().toDate())
  const valid =
    !!start &&
    !!end &&
    dayjs(start).startOf('day').valueOf() <= dayjs(end).endOf('day').valueOf()
  const params = {
    customer_id: props.customer.customer_id,
    start_timestamp: dayjs(start).startOf('day').unix(),
    end_timestamp: dayjs(end).endOf('day').unix(),
  }
  const logs = useQuery({
    queryKey: ['customers', 'log-analysis', params],
    queryFn: ({ signal }) => getCustomerLogAnalysis(params, signal),
    enabled: valid,
    retry: false,
    meta: { errorToast: false },
  })
  const cache = useQuery({
    queryKey: ['customers', 'cache-analysis', params],
    queryFn: ({ signal }) => getCustomerCacheAnalysis(params, signal),
    enabled: valid,
    retry: false,
    meta: { errorToast: false },
  })

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Customer statistics')}
      description={`${props.customer.name || props.customer.customer_id} · ${props.customer.customer_id}`}
      contentClassName='sm:max-w-4xl'
      bodyClassName='space-y-5'
      footer={
        <Button variant='outline' onClick={props.onClose}>
          {t('Close')}
        </Button>
      }
    >
      <div className='flex flex-wrap items-end gap-3'>
        <div role='group' aria-label={t('Start date')} className='space-y-1'>
          <p className='text-sm'>{t('Start date')}</p>
          <DatePicker selected={start} onSelect={setStart} />
        </div>
        <div role='group' aria-label={t('End date')} className='space-y-1'>
          <p className='text-sm'>{t('End date')}</p>
          <DatePicker selected={end} onSelect={setEnd} />
        </div>
        <Button
          variant='outline'
          disabled={!valid || logs.isFetching || cache.isFetching}
          onClick={() => {
            void logs.refetch()
            void cache.refetch()
          }}
        >
          {t('Refresh')}
        </Button>
      </div>
      {!valid && <p role='alert'>{t('Select a valid date range')}</p>}
      {valid && (
        <>
          {logs.isPending && <LoadingState />}
          {logs.isError && (
            <ErrorState
              title={t('Failed to load analysis')}
              description={getServerErrorMessage(logs.error)}
              onRetry={() => void logs.refetch()}
            />
          )}
          {logs.data && <CustomerStatistics data={logs.data} />}
          <section className='space-y-3' aria-label={t('Cache loss breakdown')}>
            <h3 className='text-sm font-medium'>{t('Cache loss breakdown')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Cache loss analysis covers complete sessions matching this customer and date range; shared sessions may include other customers and turns outside the range.'
              )}
            </p>
            {cache.isPending && <LoadingState />}
            {cache.isError && (
              <ErrorState
                title={t('Failed to load analysis')}
                description={getServerErrorMessage(cache.error)}
                onRetry={() => void cache.refetch()}
              />
            )}
            {cache.data && !cache.data.enabled && (
              <EmptyState
                title={t('Cache analysis is unavailable on this deployment')}
              />
            )}
            {cache.data?.enabled && (
              <>
                {cache.data.truncated && (
                  <p role='status' className='text-muted-foreground text-sm'>
                    {t('partial sample (scan limit reached)')}
                  </p>
                )}
                {!cache.data.loss_summary?.analyzed_turns && (
                  <EmptyState
                    title={t('No analyzable Claude cache attempts')}
                  />
                )}
                <ByteCacheCoverageSummary coverage={cache.data.coverage} />
                <CacheLossBreakdown summary={cache.data.loss_summary} />
              </>
            )}
          </section>
        </>
      )}
    </Dialog>
  )
}
