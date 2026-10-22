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
import { RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'

export interface ErrorStatsFilterDraft {
  start?: Date
  end?: Date
  statusCode: string
  model: string
  channel: string
  group: string
  limit: string
}

interface ErrorStatsFilterBarProps {
  draft: ErrorStatsFilterDraft
  onDraftChange: (next: Partial<ErrorStatsFilterDraft>) => void
  onSearch: () => void
  isFetching: boolean
}

export function ErrorStatsFilterBar(props: ErrorStatsFilterBarProps) {
  const { t } = useTranslation()

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault()
      props.onSearch()
    }
  }

  return (
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
          onChange={(e) => props.onDraftChange({ statusCode: e.target.value })}
          onKeyDown={handleKeyDown}
          placeholder='400'
          className='h-8'
        />
      </div>

      <div className='w-[180px] space-y-1.5'>
        <Label
          htmlFor='error-stats-model'
          className='text-muted-foreground text-xs'
        >
          {t('Model Name')}
        </Label>
        <Input
          id='error-stats-model'
          value={props.draft.model}
          onChange={(e) => props.onDraftChange({ model: e.target.value })}
          onKeyDown={handleKeyDown}
          className='h-8'
        />
      </div>

      <div className='w-[140px] space-y-1.5'>
        <Label
          htmlFor='error-stats-channel'
          className='text-muted-foreground text-xs'
        >
          {t('Channel ID')}
        </Label>
        <Input
          id='error-stats-channel'
          inputMode='numeric'
          value={props.draft.channel}
          onChange={(e) => props.onDraftChange({ channel: e.target.value })}
          onKeyDown={handleKeyDown}
          className='h-8'
        />
      </div>

      <div className='w-[140px] space-y-1.5'>
        <Label
          htmlFor='error-stats-group'
          className='text-muted-foreground text-xs'
        >
          {t('Group')}
        </Label>
        <Input
          id='error-stats-group'
          value={props.draft.group}
          onChange={(e) => props.onDraftChange({ group: e.target.value })}
          onKeyDown={handleKeyDown}
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
  )
}
