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
import { RotateCw } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getEnabledModels } from '@/features/channels/api'
import { CustomerFilterCombobox } from '@/features/customers/components/customer-filter-combobox'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import { getGroups } from '@/features/users/api'

export interface ErrorStatsFilterDraft {
  start?: Date
  end?: Date
  statusCode: string
  model: string
  channel: string
  group: string
  customerName: string
  customerId: string
  shortName: string
  attribution: string
  limit: string
}

interface ErrorStatsFilterBarProps {
  draft: ErrorStatsFilterDraft
  onDraftChange: (next: Partial<ErrorStatsFilterDraft>) => void
  onSearch: () => void
  isFetching: boolean
  // Channel options derived from the current result set, so the dropdown offers
  // the channels that actually produced errors while still allowing a manually
  // typed id.
  channelOptions: ComboboxInputOption[]
  // Short-name (明细错误类型) and attribution (错误归因) suggestions, drawn from the
  // configured aliases plus the current rows; manual input stays available.
  shortNameOptions: string[]
  attributionOptions: string[]
}

export function ErrorStatsFilterBar(props: ErrorStatsFilterBarProps) {
  const { t } = useTranslation()

  // Model and group dropdowns are backed by the same sources the usage-log
  // filters use; manual input stays available via allowCustomValue.
  const modelCandidatesQuery = useQuery({
    queryKey: ['error-stats-filter-models'],
    queryFn: getEnabledModels,
    staleTime: 5 * 60 * 1000,
  })
  const groupCandidatesQuery = useQuery({
    queryKey: ['error-stats-filter-groups'],
    queryFn: getGroups,
    staleTime: 5 * 60 * 1000,
  })
  const modelOptions = useMemo<ComboboxInputOption[]>(
    () =>
      (modelCandidatesQuery.data?.data ?? []).map((model) => ({
        value: model,
        label: model,
      })),
    [modelCandidatesQuery.data]
  )
  const groupOptions = useMemo<ComboboxInputOption[]>(
    () =>
      (groupCandidatesQuery.data?.data ?? []).map((group) => ({
        value: group,
        label: group,
      })),
    [groupCandidatesQuery.data]
  )
  const shortNameComboOptions = useMemo<ComboboxInputOption[]>(
    () => props.shortNameOptions.map((name) => ({ value: name, label: name })),
    [props.shortNameOptions]
  )
  const attributionComboOptions = useMemo<ComboboxInputOption[]>(
    () => props.attributionOptions.map((value) => ({ value, label: value })),
    [props.attributionOptions]
  )

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault()
      props.onSearch()
    }
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex flex-wrap items-end gap-3'>
        <div className='min-w-[260px] flex-1 space-y-1.5'>
          <Label
            htmlFor='error-stats-range'
            className='text-muted-foreground text-xs'
          >
            {t('Date Range')}
          </Label>
          <CompactDateTimeRangePicker
            start={props.draft.start}
            end={props.draft.end}
            onChange={(range) =>
              props.onDraftChange({ start: range.start, end: range.end })
            }
          />
        </div>

        <div className='w-[120px] space-y-1.5'>
          <Label
            htmlFor='error-stats-status-code'
            className='text-muted-foreground text-xs'
          >
            {t('Status Code')}
          </Label>
          <Input
            id='error-stats-status-code'
            inputMode='numeric'
            value={props.draft.statusCode}
            onChange={(e) =>
              props.onDraftChange({ statusCode: e.target.value })
            }
            onKeyDown={handleKeyDown}
            placeholder='400'
            className='h-8'
          />
        </div>

        <div className='w-[180px] space-y-1.5'>
          <Label className='text-muted-foreground text-xs'>
            {t('Model Name')}
          </Label>
          <Combobox
            options={modelOptions}
            value={props.draft.model}
            onValueChange={(value) =>
              props.onDraftChange({ model: value ?? '' })
            }
            placeholder={t('Model Name')}
            searchPlaceholder={t('Model Name')}
            emptyText={t('No option found.')}
            allowCustomValue
            clearable
            className='h-8'
          />
        </div>

        <div className='w-[180px] space-y-1.5'>
          <Label className='text-muted-foreground text-xs'>
            {t('Channel ID')}
          </Label>
          <Combobox
            options={props.channelOptions}
            value={props.draft.channel}
            onValueChange={(value) =>
              props.onDraftChange({ channel: value ?? '' })
            }
            placeholder={t('Channel ID')}
            searchPlaceholder={t('Channel ID')}
            emptyText={t('No option found.')}
            allowCustomValue
            clearable
            className='h-8'
          />
        </div>

        <div className='w-[160px] space-y-1.5'>
          <Label className='text-muted-foreground text-xs'>{t('Group')}</Label>
          <Combobox
            options={groupOptions}
            value={props.draft.group}
            onValueChange={(value) =>
              props.onDraftChange({ group: value ?? '' })
            }
            placeholder={t('Group')}
            searchPlaceholder={t('Group')}
            emptyText={t('No option found.')}
            allowCustomValue
            clearable
            className='h-8'
          />
        </div>

        <div className='w-[180px] space-y-1.5'>
          <Label className='text-muted-foreground text-xs'>
            {t('Customer Name')}
          </Label>
          <CustomerFilterCombobox
            field='name'
            value={props.draft.customerName}
            onValueChange={(value) =>
              props.onDraftChange({ customerName: value ?? '' })
            }
            className='h-8'
          />
        </div>

        <div className='w-[180px] space-y-1.5'>
          <Label className='text-muted-foreground text-xs'>
            {t('Customer ID')}
          </Label>
          <CustomerFilterCombobox
            field='id'
            value={props.draft.customerId}
            onValueChange={(value) =>
              props.onDraftChange({ customerId: value ?? '' })
            }
            className='h-8'
          />
        </div>

        <div className='w-[110px] space-y-1.5'>
          <Label
            htmlFor='error-stats-limit'
            className='text-muted-foreground text-xs'
          >
            {t('Limit')}
          </Label>
          <Input
            id='error-stats-limit'
            inputMode='numeric'
            value={props.draft.limit}
            onChange={(e) => props.onDraftChange({ limit: e.target.value })}
            onKeyDown={handleKeyDown}
            placeholder='200'
            className='h-8'
          />
        </div>

        <Button
          type='button'
          onClick={props.onSearch}
          disabled={props.isFetching}
          className='h-8'
        >
          <RotateCw
            className={props.isFetching ? 'size-4 animate-spin' : 'size-4'}
            aria-hidden='true'
          />
          {t('Search')}
        </Button>
      </div>

      <div className='flex flex-wrap items-end gap-3'>
        <div className='w-[220px] space-y-1.5'>
          <Label className='text-muted-foreground text-xs'>
            {t('Detailed Error Type')}
          </Label>
          <Combobox
            options={shortNameComboOptions}
            value={props.draft.shortName}
            onValueChange={(value) =>
              props.onDraftChange({ shortName: value ?? '' })
            }
            placeholder={t('Detailed Error Type')}
            searchPlaceholder={t('Detailed Error Type')}
            emptyText={t('No option found.')}
            allowCustomValue
            clearable
            className='h-8'
          />
        </div>

        <div className='w-[220px] space-y-1.5'>
          <Label className='text-muted-foreground text-xs'>
            {t('Error Attribution')}
          </Label>
          <Combobox
            options={attributionComboOptions}
            value={props.draft.attribution}
            onValueChange={(value) =>
              props.onDraftChange({ attribution: value ?? '' })
            }
            placeholder={t('Error Attribution')}
            searchPlaceholder={t('Error Attribution')}
            emptyText={t('No option found.')}
            allowCustomValue
            clearable
            className='h-8'
          />
        </div>
      </div>
    </div>
  )
}
