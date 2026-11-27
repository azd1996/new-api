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
import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DateTimePicker } from '@/components/datetime-picker'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { handleServerError } from '@/lib/handle-server-error'

import { getCurrentLogCleanupTask, getSystemTask } from '../api'
import type { LogCleanupTask } from '../types'

const HOURS_IN_DAY = 24

const getDateHoursAgo = (hours: number) => {
  const date = new Date()
  date.setHours(date.getHours() - hours)
  return date
}

const getDateDaysAgo = (days: number) => getDateHoursAgo(days * HOURS_IN_DAY)

const quickSelectOptions = [
  {
    label: '24 hours ago',
    getValue: () => getDateHoursAgo(24),
  },
  {
    label: '7 days ago',
    getValue: () => getDateDaysAgo(7),
  },
  {
    label: '30 days ago',
    getValue: () => getDateDaysAgo(30),
  },
]

function isActiveLogCleanupTask(task: LogCleanupTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

/**
 * Manual cleanup is disabled; log retention is managed through ClickHouse TTL.
 * Keep existing task progress visible without allowing new cleanup requests.
 */
export function HistoryLogCleanup() {
  const { t } = useTranslation()
  const titleId = useId()
  const [purgeDate, setPurgeDate] = useState<Date | undefined>(() =>
    getDateDaysAgo(30)
  )
  const [logCleanupTask, setLogCleanupTask] = useState<LogCleanupTask | null>(
    null
  )

  useEffect(() => {
    let cancelled = false

    async function fetchCurrentLogCleanupTask() {
      try {
        const res = await getCurrentLogCleanupTask()
        if (!cancelled && res.success && res.data) {
          setLogCleanupTask(res.data)
        }
      } catch {
        /* ignore */
      }
    }

    fetchCurrentLogCleanupTask()

    return () => {
      cancelled = true
    }
  }, [])

  const logCleanupActive = isActiveLogCleanupTask(logCleanupTask)
  const logCleanupState = logCleanupTask?.state
  const logCleanupProgress = Math.min(
    100,
    Math.max(0, logCleanupState?.progress ?? 0)
  )
  const logCleanupProcessed = logCleanupState?.processed ?? 0
  const logCleanupTotal = logCleanupState?.total ?? 0
  const logCleanupTaskId = logCleanupTask?.task_id

  useEffect(() => {
    if (!logCleanupTaskId || !logCleanupActive) return

    let cancelled = false
    const interval = window.setInterval(async () => {
      try {
        const res = await getSystemTask(logCleanupTaskId)
        if (cancelled || !res.success || !res.data) return

        setLogCleanupTask(res.data)
        if (!isActiveLogCleanupTask(res.data)) {
          if (res.data.status === 'succeeded') {
            const count =
              res.data.result?.deleted_count ?? res.data.state?.processed ?? 0
            toast.success(
              count > 0
                ? t('{{count}} log entries removed.', { count })
                : t('No log entries matched the selected time.')
            )
          } else if (res.data.status === 'failed') {
            handleServerError(res.data, t('Failed to clean logs'))
          }
        }
      } catch {
        /* keep polling */
      }
    }, 1000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [logCleanupActive, logCleanupTaskId, t])

  return (
    <div
      role='group'
      aria-labelledby={titleId}
      className='flex min-w-0 flex-col gap-4'
    >
      <div>
        <h4 id={titleId} className='font-medium'>
          {t('Clean history logs')}
        </h4>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t('Remove all log entries created before the selected timestamp.')}
        </p>
      </div>
      <DateTimePicker value={purgeDate} onChange={setPurgeDate} />
      <div className='flex flex-wrap gap-3'>
        {quickSelectOptions.map((option) => (
          <Button
            key={option.label}
            type='button'
            variant='outline'
            onClick={() => setPurgeDate(option.getValue())}
          >
            {t(option.label)}
          </Button>
        ))}
        <Button
          type='button'
          variant='secondary'
          className='bg-muted text-muted-foreground'
          disabled
        >
          {logCleanupActive ? t('Cleaning...') : t('Clean logs')}
        </Button>
      </div>
      {logCleanupTask && (
        <div className='rounded-md border p-3'>
          <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
            <span className='font-medium'>{t('Log cleanup progress')}</span>
            <span className='text-muted-foreground tabular-nums'>
              {logCleanupProgress}%
            </span>
          </div>
          <Progress value={logCleanupProgress} />
          <div className='text-muted-foreground mt-2 text-xs'>
            {t('{{processed}} of {{total}} log entries processed.', {
              processed: logCleanupProcessed,
              total: logCleanupTotal,
            })}
          </div>
          {logCleanupTask.status === 'failed' && logCleanupTask.error && (
            <div className='text-destructive mt-2 text-xs'>
              {logCleanupTask.error}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
