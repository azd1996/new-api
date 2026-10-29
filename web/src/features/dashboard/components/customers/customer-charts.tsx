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
import { VChart } from '@visactor/react-vchart'
import { Building2, Loader2 } from 'lucide-react'
import { useEffect, useMemo, useState, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useTheme } from '@/context/theme-provider'
import { getCustomerQuotaData } from '@/features/dashboard/api'
import {
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import {
  getDefaultDays,
  saveGranularity,
  processCustomerChartData,
} from '@/features/dashboard/lib'
import type {
  CustomerAmountMetric,
  CustomerChartsFilters,
  CustomerGroupBy,
  ProcessedCustomerChartData,
} from '@/features/dashboard/types'
import { getRollingDateRange, type TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

const CUSTOMER_CHARTS: {
  value: string
  labelKey: string
  specKey: keyof ProcessedCustomerChartData
}[] = [
  {
    value: 'rank',
    labelKey: 'Customer Consumption Ranking',
    specKey: 'spec_customer_rank',
  },
  {
    value: 'trend',
    labelKey: 'Customer Consumption Trend',
    specKey: 'spec_customer_trend',
  },
]

const TOP_CUSTOMER_LIMIT_OPTIONS = [5, 10, 20, 50]

// Customer statistics is always scoped to the per-customer discounted amount
// (with channel-discount fallback for unconfigured customers); other calibers
// are meaningless here. The only toggle is whether to group by the customer's
// configured name or its raw customer id.
const CUSTOMER_GROUP_BY_OPTIONS = [
  { value: 'name', labelKey: 'By customer name' },
  { value: 'id', labelKey: 'By customer ID' },
] as const

const CUSTOMER_AMOUNT_OPTIONS: {
  value: CustomerAmountMetric
  labelKey: string
}[] = [
  { value: 'customer_stat_quota', labelKey: 'Customer discounted amount' },
  { value: 'channel_stat_quota', labelKey: 'Channel discounted amount' },
  { value: 'profit_quota', labelKey: 'Profit amount' },
  { value: 'profit_rate', labelKey: 'Profit rate' },
]

const CUSTOMER_AMOUNT_DESCRIPTIONS: Record<CustomerAmountMetric, string> = {
  customer_stat_quota:
    'Customer discounted amount uses the channel discounted amount as a fallback for unconfigured customers.',
  channel_stat_quota:
    'Channel discounted amount is calculated using the channel discount.',
  profit_quota:
    'Profit amount is the customer discounted amount minus the channel discounted amount.',
  profit_rate:
    'Profit rate is profit amount divided by channel discounted amount.',
}

interface CustomerChartsProps {
  filters: CustomerChartsFilters
  onFiltersChange: (filters: CustomerChartsFilters) => void
}

export function CustomerCharts(props: CustomerChartsProps) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  // Selections are owned by the dashboard parent so they persist across
  // sub-section switches; the rolling window is derived from the chosen range.
  const timeGranularity = props.filters.timeGranularity
  const selectedRange = props.filters.selectedRange
  const topCustomerLimit = props.filters.topCustomerLimit
  const groupBy = props.filters.groupBy
  const amountMetric = props.filters.amountMetric
  const onFiltersChange = props.onFiltersChange

  const timeRange = useMemo(() => {
    const { start, end } = getRollingDateRange(selectedRange)
    return {
      start_timestamp: Math.floor(start.getTime() / 1000),
      end_timestamp: Math.floor(end.getTime() / 1000),
    }
  }, [selectedRange])

  const handleRangeChange = useCallback(
    (days: number) => {
      onFiltersChange({ ...props.filters, selectedRange: days })
    },
    [onFiltersChange, props.filters]
  )

  const handleGranularityChange = useCallback(
    (g: TimeGranularity) => {
      saveGranularity(g)
      onFiltersChange({
        ...props.filters,
        timeGranularity: g,
        selectedRange: getDefaultDays(g),
      })
    },
    [onFiltersChange, props.filters]
  )

  const handleTopLimitChange = useCallback(
    (limit: number) => {
      onFiltersChange({ ...props.filters, topCustomerLimit: limit })
    },
    [onFiltersChange, props.filters]
  )

  const handleGroupByChange = useCallback(
    (value: CustomerGroupBy) => {
      onFiltersChange({ ...props.filters, groupBy: value })
    },
    [onFiltersChange, props.filters]
  )

  const handleAmountMetricChange = useCallback(
    (value: CustomerAmountMetric) => {
      onFiltersChange({ ...props.filters, amountMetric: value })
    },
    [onFiltersChange, props.filters]
  )

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    updateTheme()
  }, [resolvedTheme])

  const { data: customerData, isLoading } = useQuery({
    queryKey: ['dashboard', 'customer-quota', timeRange],
    queryFn: () => getCustomerQuotaData(timeRange),
    select: (res) => (res.success ? res.data : []),
    staleTime: 60_000,
  })

  const chartData = useMemo(
    () =>
      processCustomerChartData(
        isLoading ? [] : (customerData ?? []),
        timeGranularity,
        t,
        topCustomerLimit,
        groupBy,
        amountMetric
      ),
    [
      customerData,
      isLoading,
      timeGranularity,
      t,
      topCustomerLimit,
      groupBy,
      amountMetric,
    ]
  )

  return (
    <div className='space-y-3'>
      <div className='flex items-center gap-1.5 overflow-x-auto pb-1 sm:gap-2'>
        <Tabs
          value={String(selectedRange)}
          onValueChange={(value) => handleRangeChange(Number(value))}
          className='shrink-0'
        >
          <TabsList>
            {TIME_RANGE_PRESETS.map((preset) => (
              <TabsTrigger
                key={preset.days}
                value={String(preset.days)}
                className='px-2.5 text-xs'
              >
                {t(preset.label)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <Tabs
          value={timeGranularity}
          onValueChange={(value) =>
            handleGranularityChange(value as TimeGranularity)
          }
          className='shrink-0'
        >
          <TabsList>
            {TIME_GRANULARITY_OPTIONS.map((opt) => (
              <TabsTrigger
                key={opt.value}
                value={opt.value}
                className='px-2.5 text-xs'
              >
                {t(opt.label)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <Tabs
          value={String(topCustomerLimit)}
          onValueChange={(value) => handleTopLimitChange(Number(value))}
          className='shrink-0'
        >
          <TabsList>
            <span className='text-muted-foreground px-2 text-xs font-medium whitespace-nowrap'>
              {t('Top Customers')}
            </span>
            {TOP_CUSTOMER_LIMIT_OPTIONS.map((limit) => (
              <TabsTrigger
                key={limit}
                value={String(limit)}
                className='px-2.5 text-xs'
              >
                {t('Top {{count}}', { count: limit })}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <Tabs
          value={groupBy}
          onValueChange={(value) =>
            handleGroupByChange(value as CustomerGroupBy)
          }
          className='shrink-0'
        >
          <TabsList>
            {CUSTOMER_GROUP_BY_OPTIONS.map((opt) => (
              <TabsTrigger
                key={opt.value}
                value={opt.value}
                className='px-2.5 text-xs'
              >
                {t(opt.labelKey)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <Tabs
          value={amountMetric}
          onValueChange={(value) =>
            handleAmountMetricChange(value as CustomerAmountMetric)
          }
          className='shrink-0'
        >
          <TabsList>
            {CUSTOMER_AMOUNT_OPTIONS.map((opt) => (
              <TabsTrigger
                key={opt.value}
                value={opt.value}
                className='px-2.5 text-xs'
              >
                {t(opt.labelKey)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        {isLoading && (
          <Loader2 className='text-muted-foreground size-4 animate-spin' />
        )}
      </div>

      <p className='text-muted-foreground text-xs'>
        {t(CUSTOMER_AMOUNT_DESCRIPTIONS[amountMetric])}
      </p>

      <div className='grid gap-3'>
        {CUSTOMER_CHARTS.map((chart) => {
          const spec = chartData[chart.specKey]

          return (
            <div
              key={chart.value}
              className='overflow-hidden rounded-lg border'
            >
              <div className='flex w-full items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
                <IconBadge tone='info' size='sm'>
                  <Building2 />
                </IconBadge>
                <div className='text-sm font-semibold'>{t(chart.labelKey)}</div>
              </div>

              <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>
                {isLoading ? (
                  <Skeleton className='h-full w-full' />
                ) : (
                  themeReady &&
                  spec && (
                    <VChart
                      key={`customer-${chart.value}-${topCustomerLimit}-${groupBy}-${amountMetric}-${resolvedTheme}`}
                      spec={{
                        ...spec,
                        theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                        background: 'transparent',
                      }}
                      option={{ ...VCHART_OPTION, renderMode: 'svg' }}
                    />
                  )
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
