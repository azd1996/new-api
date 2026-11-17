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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import {
  applyUserQuotaPolicy,
  getUserQuotaPolicy,
  saveUserQuotaPolicy,
} from '../api'
import type { UserQuotaPolicyMode } from '../types'

interface UserQuotaPolicyDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number
  currentQuota: number
  onSuccess: () => void
}

type PolicyModeChoice = UserQuotaPolicyMode | 'off'

const MODE_CHOICES: PolicyModeChoice[] = ['off', 'monthly_refresh', 'fixed']

// The refresh runs on the first day of every calendar month, so the next run is
// always the first day of the next month.
function nextRefreshDate(now: Date): string {
  const next = new Date(now.getFullYear(), now.getMonth() + 1, 1)
  const month = String(next.getMonth() + 1).padStart(2, '0')
  return `${next.getFullYear()}-${month}-01`
}

export function UserQuotaPolicyDialog(props: UserQuotaPolicyDialogProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<PolicyModeChoice>('off')
  const [amount, setAmount] = useState('')
  const [loading, setLoading] = useState(false)
  const [confirmApplyOpen, setConfirmApplyOpen] = useState(false)

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'

  const policyQuery = useQuery({
    queryKey: ['users', 'quota-policy', props.userId],
    queryFn: () => getUserQuotaPolicy(props.userId),
    enabled: props.open,
  })

  useEffect(() => {
    if (!props.open || !policyQuery.data) return
    const policy = policyQuery.data.data?.policy
    if (!policy) {
      setMode('off')
      setAmount('')
      return
    }
    setMode(policy.mode)
    setAmount(String(policy.amount))
  }, [props.open, policyQuery.data])

  const policy = policyQuery.data?.data?.policy ?? null
  const quotaValue = parseQuotaFromDollars(Number.parseFloat(amount) || 0)

  const handleSave = async () => {
    if (mode !== 'off' && quotaValue <= 0) return
    setLoading(true)
    try {
      const result = await saveUserQuotaPolicy({
        user_id: props.userId,
        mode: mode === 'off' ? '' : mode,
        amount: mode === 'off' ? 0 : quotaValue,
      })
      if (result.success) {
        toast.success(t('Quota policy saved'))
        props.onOpenChange(false)
        props.onSuccess()
      } else {
        handleServerError(result, t('Failed to save quota policy'))
      }
    } catch (error: unknown) {
      handleServerError(error, t('Failed to save quota policy'))
    } finally {
      setLoading(false)
    }
  }

  const handleApply = async () => {
    setLoading(true)
    try {
      const result = await applyUserQuotaPolicy(props.userId)
      if (result.success) {
        toast.success(t('Quota granted for this month'))
        setConfirmApplyOpen(false)
        props.onSuccess()
      } else {
        handleServerError(result, t('Failed to grant quota'))
      }
    } catch (error: unknown) {
      handleServerError(error, t('Failed to grant quota'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={t('Quota policy')}
        description={t(
          'Choose how the balance of this user is maintained over time'
        )}
        contentHeight='auto'
        bodyClassName='space-y-4'
        footer={
          <>
            <Button variant='outline' onClick={() => props.onOpenChange(false)}>
              {t('Cancel')}
            </Button>
            <Button onClick={handleSave} disabled={loading}>
              {loading ? t('Processing...') : t('Save')}
            </Button>
          </>
        }
      >
        <div className='space-y-4'>
          <div className='space-y-2'>
            <Label>{t('Mode')}</Label>
            <div className='flex gap-1'>
              {MODE_CHOICES.map((choice) => (
                <Button
                  key={choice}
                  type='button'
                  variant='outline'
                  size='sm'
                  className={cn(
                    mode === choice &&
                      'bg-primary text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground'
                  )}
                  onClick={() => setMode(choice)}
                >
                  {choice === 'off' && t('Off')}
                  {choice === 'monthly_refresh' && t('Monthly refresh')}
                  {choice === 'fixed' && t('Fixed amount')}
                </Button>
              ))}
            </div>
          </div>

          {mode === 'off' ? (
            <p className='text-muted-foreground text-sm'>
              {t(
                'No policy is stored for this user; the balance only changes through top-ups and adjustments.'
              )}
            </p>
          ) : (
            <>
              <div className='space-y-2'>
                <Label htmlFor='quota-policy-amount'>
                  {t('Amount')} ({currencyLabel})
                </Label>
                <Input
                  id='quota-policy-amount'
                  type='number'
                  step={tokensOnly ? 1 : 0.000001}
                  min={0}
                  placeholder={
                    tokensOnly
                      ? t('Enter amount in tokens')
                      : t('Enter amount in {{currency}}', {
                          currency: currencyLabel,
                        })
                  }
                  value={amount}
                  onChange={(event) => setAmount(event.target.value)}
                />
              </div>

              <div className='text-muted-foreground space-y-1 text-sm'>
                <div>
                  {t('Current quota')}: {formatQuota(props.currentQuota)}
                  {mode === 'monthly_refresh' &&
                    ` → ${formatQuota(quotaValue)}`}
                </div>
                {mode === 'monthly_refresh' ? (
                  <>
                    <div>
                      {t('Next refresh')}: {nextRefreshDate(new Date())}
                    </div>
                    {policy?.last_granted_period && (
                      <div>
                        {t('Last granted period')}: {policy.last_granted_period}
                      </div>
                    )}
                    <div>
                      {t(
                        'The balance is overwritten on the first day of every month; unused quota and any topped-up balance are cleared.'
                      )}
                    </div>
                  </>
                ) : (
                  <div>
                    {t(
                      'The amount is applied once and never changes again automatically.'
                    )}
                  </div>
                )}
              </div>

              {mode === 'monthly_refresh' && (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => setConfirmApplyOpen(true)}
                >
                  {t('Grant this month now')}
                </Button>
              )}
            </>
          )}
        </div>
      </Dialog>

      <ConfirmDialog
        open={confirmApplyOpen}
        onOpenChange={setConfirmApplyOpen}
        title={t('Grant this month now')}
        desc={t(
          'Overwrite the balance of this user now instead of waiting for the next month?'
        )}
        confirmText={t('Confirm')}
        handleConfirm={handleApply}
      />
    </>
  )
}
