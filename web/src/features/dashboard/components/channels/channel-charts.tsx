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
import { Boxes, Loader2 } from 'lucide-react'
import { useEffect, useMemo, useState, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useTheme } from '@/context/theme-provider'
import { getChannelQuotaData } from '@/features/dashboard/api'
import {
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import {
  getDefaultDays,
  saveGranularity,
  processChannelChartData,
} from '@/features/dashboard/lib'
import type {
  ChannelAmountMetric,
  ChannelChartsFilters,
  ProcessedChannelChartData,
} from '@/features/dashboard/types'
import { getRollingDateRange, type TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

const CHANNEL_CHARTS: {
  value: string
  labelKey: string
  specKey: keyof ProcessedChannelChartData
}[] = [
  {
    value: 'rank',
    labelKey: 'Channel Consumption Ranking',
    specKey: 'spec_channel_rank',
  },
  {
    value: 'trend',
    labelKey: 'Channel Consumption Trend',
    specKey: 'spec_channel_trend',
  },
]

const TOP_CHANNEL_LIMIT_OPTIONS = [5, 10, 20, 50]

const CHANNEL_AMOUNT_OPTIONS: {
  value: ChannelAmountMetric
  labelKey: string
}[] = [
  { value: 'channel_stat_quota', labelKey: 'Channel discounted amount' },
  { value: 'customer_stat_quota', labelKey: 'Customer discounted amount' },
  { value: 'profit_quota', labelKey: 'Profit amount' },
]

const CHANNEL_AMOUNT_DESCRIPTIONS: Record<ChannelAmountMetric, string> = {
  channel_stat_quota:
    'Channel discounted amount is calculated using the channel discount.',
  customer_stat_quota:
    'Customer discounted amount is the amount charged to customers after their discount.',
  profit_quota:
    'Profit amount is the customer discounted amount minus the channel discounted amount.',
}

interface ChannelChartsProps {
  filters: ChannelChartsFilters
  onFiltersChange: (filters: ChannelChartsFilters) => void
}

export function ChannelCharts(props: ChannelChartsProps) {
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
  const topChannelLimit = props.filters.topChannelLimit
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
      onFiltersChange({ ...props.filters, topChannelLimit: limit })
    },
    [onFiltersChange, props.filters]
  )

  const handleAmountMetricChange = useCallback(
    (value: ChannelAmountMetric) => {
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

  const { data: channelData, isLoading } = useQuery({
    queryKey: ['dashboard', 'channel-quota', timeRange],
    queryFn: () => getChannelQuotaData(timeRange),
    select: (res) => (res.success ? res.data : []),
    staleTime: 60_000,
  })

  const chartData = useMemo(
    () =>
      processChannelChartData(
        isLoading ? [] : (channelData ?? []),
        timeGranularity,
        t,
        topChannelLimit,
        amountMetric
      ),
    [channelData, isLoading, timeGranularity, t, topChannelLimit, amountMetric]
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
          value={String(topChannelLimit)}
          onValueChange={(value) => handleTopLimitChange(Number(value))}
          className='shrink-0'
        >
          <TabsList>
            <span className='text-muted-foreground px-2 text-xs font-medium whitespace-nowrap'>
              {t('Top Channels')}
            </span>
            {TOP_CHANNEL_LIMIT_OPTIONS.map((limit) => (
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
          value={amountMetric}
          onValueChange={(value) =>
            handleAmountMetricChange(value as ChannelAmountMetric)
          }
          className='shrink-0'
        >
          <TabsList>
            {CHANNEL_AMOUNT_OPTIONS.map((opt) => (
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
        {t(CHANNEL_AMOUNT_DESCRIPTIONS[amountMetric])}
      </p>

      <div className='grid gap-3'>
        {CHANNEL_CHARTS.map((chart) => {
          const spec = chartData[chart.specKey]

          return (
            <div
              key={chart.value}
              className='overflow-hidden rounded-lg border'
            >
              <div className='flex w-full items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
                <IconBadge tone='info' size='sm'>
                  <Boxes />
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
                      key={`channel-${chart.value}-${topChannelLimit}-${amountMetric}-${resolvedTheme}`}
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
