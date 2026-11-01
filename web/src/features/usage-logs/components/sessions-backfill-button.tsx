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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { createServerError } from '@/lib/server-error-message'

import { getLogSessionsBackfillStatus, startLogSessionsBackfill } from '../api'

const BACKFILL_STATUS_KEY = ['logs', 'sessions', 'backfill-status']

// SessionsBackfillButton is a temporary root-only control for loading
// history into the session table. The run is throttled server-side but still
// scans logs, so the dialog asks to start it at a quiet hour. The status
// comes from whichever node serves the request.
export function SessionsBackfillButton() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [days, setDays] = useState('7')

  const status = useQuery({
    queryKey: BACKFILL_STATUS_KEY,
    queryFn: async () => {
      const result = await getLogSessionsBackfillStatus()
      if (!result?.success) {
        throw createServerError(result, t('Failed to load backfill status'))
      }
      return result.data
    },
    refetchInterval: (query) => (query.state.data?.running ? 3000 : false),
  })

  const start = useMutation({
    mutationFn: async (value: number) => {
      const result = await startLogSessionsBackfill(value)
      if (!result?.success) {
        throw createServerError(result, t('Failed to start backfill'))
      }
      return result.data
    },
    onSuccess: (data) => {
      queryClient.setQueryData(BACKFILL_STATUS_KEY, data)
      setOpen(false)
      toast.success(t('Backfill started'))
    },
  })

  const parsedDays = Number(days)
  const daysValid =
    Number.isInteger(parsedDays) && parsedDays >= 1 && parsedDays <= 366
  const state = status.data
  const doneCount = state?.done_days?.length ?? 0
  const skippedCount = state?.skipped_days?.length ?? 0

  let statusText = ''
  if (state?.running) {
    statusText = t('Backfilling: {{done}} / {{days}} days done', {
      done: doneCount + skippedCount,
      days: state.days,
    })
  } else if (state?.last_error) {
    statusText = t('Backfill failed: {{error}}', { error: state.last_error })
  } else if (state && state.days > 0) {
    statusText = t(
      'Backfill finished: {{done}} days loaded, {{skipped}} skipped',
      {
        done: doneCount,
        skipped: skippedCount,
      }
    )
  }

  return (
    <div className='flex items-center gap-2'>
      <Button
        variant='outline'
        size='sm'
        disabled={state?.running}
        onClick={() => setOpen(true)}
      >
        {t('Backfill session history')}
      </Button>
      {statusText && (
        <span className='text-muted-foreground text-xs' role='status'>
          {statusText}
        </span>
      )}
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={t('Backfill session history')}
        desc={t(
          'Loads logs written before the session table existed, one day per step. It scans the log table, so run it at a quiet hour. Days already loaded are skipped.'
        )}
        confirmText={t('Start')}
        disabled={!daysValid}
        isLoading={start.isPending}
        handleConfirm={() => start.mutate(parsedDays)}
      >
        <div className='flex flex-col gap-2'>
          <Label htmlFor='sessions-backfill-days'>
            {t('Days to backfill (1-366)')}
          </Label>
          <Input
            id='sessions-backfill-days'
            type='number'
            min={1}
            max={366}
            value={days}
            onChange={(event) => setDays(event.target.value)}
            aria-invalid={!daysValid}
          />
        </div>
      </ConfirmDialog>
    </div>
  )
}
