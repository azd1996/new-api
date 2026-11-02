import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import type { LogAnalysisResult } from '@/features/usage-logs/types'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

export function CustomerStatistics(props: { data: LogAnalysisResult }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const percent = new Intl.NumberFormat(locale, {
    style: 'percent',
    maximumFractionDigits: 1,
  })
  const data = props.data
  const success = data.success.total
  const cache = data.cache.total
  const rate = (value: number, denominator: number) =>
    denominator > 0 ? percent.format(value) : '—'
  const metrics = [
    { label: t('Total requests'), value: formatNumber(success.total, locale) },
    { label: t('Success rate'), value: rate(success.rate, success.total) },
    {
      label: t('Gross hit rate'),
      value: rate(cache.gross_rate, cache.input_all),
    },
    {
      label: t('Eligible hit rate'),
      value: rate(cache.eligible_rate, cache.input_eligible),
    },
    {
      label: t('Success (consume)'),
      value: formatNumber(success.success, locale),
    },
    { label: t('Error'), value: formatNumber(success.error, locale) },
    {
      label: t('Retry override'),
      value: formatNumber(success.retry_override, locale),
    },
    {
      label: t('Actual cache read tokens'),
      value: formatNumber(cache.cache_read, locale),
    },
  ]

  return (
    <div className='space-y-4'>
      {data.truncated && (
        <p role='status' className='text-muted-foreground text-sm'>
          {t('partial sample (scan limit reached)')}
        </p>
      )}
      {success.total === 0 && <EmptyState title={t('No data')} />}
      <dl className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        {metrics.map((metric) => (
          <div key={metric.label} className='rounded-md border p-3'>
            <dt className='text-muted-foreground text-xs'>{metric.label}</dt>
            <dd className='mt-1 text-lg font-semibold tabular-nums'>
              {metric.value}
            </dd>
          </div>
        ))}
      </dl>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Total requests ≈ consume + error - retry-override (retried intermediate attempts); success = consume count. Success rate = success / total.'
        )}
      </p>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Rate = cache_read / total input tokens (for Claude, prompt_tokens excludes cache, so total = prompt + cache_read + cache_creation; OpenAI already includes cached tokens in prompt). Eligible excludes only too-small requests; cold writes count as misses. OpenAI cold writes cannot be detected and fall into "other miss".'
        )}
      </p>
      <h3 className='text-sm font-medium'>{t('Latency / FRT (ms)')}</h3>
      <StaticDataTable
        tableProps={{ 'aria-label': t('Latency / FRT (ms)') }}
        data={data.latency.mean.buckets}
        getRowKey={(row) => row.bucket}
        emptyContent={t('No data')}
        columns={[
          { id: 'bucket', header: t('Prompt Size'), cell: (row) => row.bucket },
          ...(
            [
              ['mean', t('Mean')],
              ['p50', t('P50 (median)')],
              ['p90', t('P90')],
              ['p99', t('P99')],
            ] as const
          ).map(([key, title]) => ({
            id: key,
            header: title,
            cell: (row: { bucket: string }) => {
              const cell = data.latency[key].buckets.find(
                (bucket) => bucket.bucket === row.bucket
              )?.total
              if (!cell || cell.cnt === 0) return '—'
              return `${formatNumber(cell.lat, locale)} / ${formatNumber(cell.frt, locale)} / ${formatNumber(cell.cnt, locale)}`
            },
          })),
        ]}
      />
      <p className='text-muted-foreground text-xs'>
        {t('Each cell: latency / FRT / count')}
      </p>
    </div>
  )
}
