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
import { Coins, Landmark } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import type { QuotaDataItem } from '@/features/dashboard/types'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

interface ConsumptionAmountCardsProps {
  data: QuotaDataItem[]
  loading?: boolean
  // When false, amounts are masked (sensitive financial figures).
  sensitiveVisible?: boolean
}

// ADMIN-ONLY reporting amounts shown next to 总额度: channel stat-group-ratio
// discounted and per-customer discounted. Both are summed from the dashboard
// quota_data rows the model chart already loaded.
export function ConsumptionAmountCards(props: ConsumptionAmountCardsProps) {
  const { t } = useTranslation()
  const sensitiveVisible = props.sensitiveVisible ?? true

  const totals = useMemo(() => {
    return (props.loading ? [] : props.data).reduce(
      (acc, item) => ({
        channel: acc.channel + (Number(item.channel_stat_quota) || 0),
        customer: acc.customer + (Number(item.customer_stat_quota) || 0),
      }),
      { channel: 0, customer: 0 }
    )
  }, [props.data, props.loading])

  const items = [
    {
      title: t('Channel Discounted Amount'),
      value: totals.channel,
      icon: Landmark,
      tone: 'warning' as const,
    },
    {
      title: t('Customer Discounted Amount'),
      value: totals.customer,
      icon: Coins,
      tone: 'success' as const,
    },
  ]

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='divide-border/60 grid min-w-0 grid-cols-2 divide-x'>
        {items.map((it) => {
          const Icon = it.icon
          return (
            <div key={it.title} className='min-w-0 px-2.5 py-1.5 sm:px-5 sm:py-4'>
              <div className='flex min-w-0 items-center gap-1.5 sm:gap-2'>
                <IconBadge
                  tone={it.tone}
                  size='stat'
                  className='size-4 rounded-sm sm:size-7 sm:rounded-md [&>svg]:size-2.5 sm:[&>svg]:size-3.5'
                >
                  <Icon />
                </IconBadge>
                <div className='text-muted-foreground truncate text-[11px] leading-4 font-medium tracking-wide uppercase sm:text-xs sm:tracking-wider'>
                  {it.title}
                </div>
              </div>
              {props.loading ? (
                <div className='mt-1 sm:mt-2'>
                  <Skeleton className='h-5 w-20 sm:h-7 sm:w-24' />
                </div>
              ) : (
                <div
                  className={cn(
                    'text-foreground mt-1 max-w-full truncate font-mono text-base leading-tight font-bold tracking-tight tabular-nums sm:mt-2 sm:text-2xl sm:leading-normal'
                  )}
                >
                  {sensitiveVisible ? formatQuota(it.value) : '••••'}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
