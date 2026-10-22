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
import { ArrowDown, ArrowUp, Plus, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import { getErrorSignatureRules, updateErrorSignatureRules } from '../api'
import type { SignatureRule } from '../types'

interface ErrorSignatureRulesDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  // When provided, these rules are appended to the fetched current rules when
  // the dialog opens, deduping by identical pattern. Used to push suggested
  // rules from the suggestions dialog into the editor.
  initialExtraRules?: SignatureRule[]
}

// Local row wrapper: rules have no stable identity from the backend, so we
// attach a client-only id to keep React keys and reordering stable.
interface RuleRow extends SignatureRule {
  _id: string
}

function newId(): string {
  return crypto.randomUUID()
}

function toRows(rules: SignatureRule[]): RuleRow[] {
  return rules.map((rule) => ({ ...rule, _id: newId() }))
}

function toRules(rows: RuleRow[]): SignatureRule[] {
  return rows.map((row) => ({
    name: row.name,
    pattern: row.pattern,
    replacement: row.replacement,
    enabled: row.enabled,
  }))
}

function emptyRow(): RuleRow {
  return { name: '', pattern: '', replacement: '', enabled: true, _id: newId() }
}

export function ErrorSignatureRulesDialog(
  props: ErrorSignatureRulesDialogProps
) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [rules, setRules] = useState<RuleRow[]>([])

  const rulesQuery = useQuery({
    queryKey: ['error-signature-rules'],
    queryFn: getErrorSignatureRules,
    enabled: props.open,
  })

  // Reset the local draft whenever the dialog opens or fresh data arrives.
  // When initialExtraRules are supplied, append the ones whose pattern is not
  // already present so suggestions can be seeded into the editor.
  useEffect(() => {
    if (props.open && rulesQuery.data?.success) {
      const base = toRows(rulesQuery.data.data ?? [])
      const extra = props.initialExtraRules ?? []
      if (extra.length === 0) {
        setRules(base)
        return
      }
      const existingPatterns = new Set(base.map((rule) => rule.pattern))
      const appended = extra
        .filter((rule) => !existingPatterns.has(rule.pattern))
        .map((rule) => ({ ...rule, _id: newId() }))
      setRules([...base, ...appended])
    }
  }, [props.open, rulesQuery.data, props.initialExtraRules])

  const saveMutation = useMutation({
    mutationFn: updateErrorSignatureRules,
    onSuccess: (res) => {
      if (res.success) {
        setRules(toRows(res.data ?? []))
        toast.success(t('Rules saved'))
        void queryClient.invalidateQueries({
          queryKey: ['error-signature-rules'],
        })
        void queryClient.invalidateQueries({ queryKey: ['error-stats'] })
        props.onOpenChange(false)
        return
      }
      toast.error(res.message || t('Failed to save rules'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to save rules'))
    },
  })

  const updateRule = (index: number, patch: Partial<SignatureRule>) =>
    setRules((prev) =>
      prev.map((rule, i) => (i === index ? { ...rule, ...patch } : rule))
    )

  const removeRule = (index: number) =>
    setRules((prev) => prev.filter((_, i) => i !== index))

  const moveRule = (index: number, direction: -1 | 1) =>
    setRules((prev) => {
      const target = index + direction
      if (target < 0 || target >= prev.length) return prev
      const next = [...prev]
      const [moved] = next.splice(index, 1)
      next.splice(target, 0, moved)
      return next
    })

  const addRule = () => setRules((prev) => [...prev, emptyRow()])

  const handleSave = () => saveMutation.mutate(toRules(rules))

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Normalization rules')}
      description={t(
        'Rules run top to bottom as regex replacements; $1 capture groups are supported.'
      )}
      contentClassName='sm:max-w-3xl'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            size='sm'
            onClick={handleSave}
            disabled={saveMutation.isPending || rulesQuery.isLoading}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='space-y-3'>
        {rulesQuery.isLoading && (
          <div className='text-muted-foreground py-6 text-center text-sm'>
            {t('Loading...')}
          </div>
        )}

        {!rulesQuery.isLoading && rules.length === 0 && (
          <div className='text-muted-foreground py-6 text-center text-sm'>
            {t('No rules configured yet.')}
          </div>
        )}

        {!rulesQuery.isLoading && rules.length > 0 && (
          <div className='space-y-3'>
            {rules.map((rule, index) => (
              <div
                key={rule._id}
                className='bg-muted/30 flex flex-col gap-3 rounded-lg border p-3'
              >
                <div className='flex items-center justify-between gap-2'>
                  <div className='flex items-center gap-2'>
                    <Switch
                      checked={rule.enabled}
                      onCheckedChange={(checked) =>
                        updateRule(index, { enabled: checked })
                      }
                      aria-label={t('Enabled')}
                    />
                    <span className='text-muted-foreground text-xs'>
                      {t('Enabled')}
                    </span>
                  </div>
                  <div className='flex items-center gap-1'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-xs'
                      aria-label={t('Move up')}
                      disabled={index === 0}
                      onClick={() => moveRule(index, -1)}
                    >
                      <ArrowUp className='size-3.5' aria-hidden='true' />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-xs'
                      aria-label={t('Move down')}
                      disabled={index === rules.length - 1}
                      onClick={() => moveRule(index, 1)}
                    >
                      <ArrowDown className='size-3.5' aria-hidden='true' />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon-xs'
                      aria-label={t('Remove')}
                      onClick={() => removeRule(index)}
                    >
                      <X className='size-3.5' aria-hidden='true' />
                    </Button>
                  </div>
                </div>

                <div className='grid gap-3 sm:grid-cols-3'>
                  <div className='space-y-1.5'>
                    <Label
                      htmlFor={`rule-name-${index}`}
                      className='text-muted-foreground text-xs'
                    >
                      {t('Rule name')}
                    </Label>
                    <Input
                      id={`rule-name-${index}`}
                      value={rule.name}
                      onChange={(e) =>
                        updateRule(index, { name: e.target.value })
                      }
                      className='h-8'
                    />
                  </div>
                  <div className='space-y-1.5'>
                    <Label
                      htmlFor={`rule-pattern-${index}`}
                      className='text-muted-foreground text-xs'
                    >
                      {t('Pattern')}
                    </Label>
                    <Input
                      id={`rule-pattern-${index}`}
                      value={rule.pattern}
                      onChange={(e) =>
                        updateRule(index, { pattern: e.target.value })
                      }
                      className='h-8 font-mono'
                    />
                  </div>
                  <div className='space-y-1.5'>
                    <Label
                      htmlFor={`rule-replacement-${index}`}
                      className='text-muted-foreground text-xs'
                    >
                      {t('Replacement')}
                    </Label>
                    <Input
                      id={`rule-replacement-${index}`}
                      value={rule.replacement}
                      onChange={(e) =>
                        updateRule(index, { replacement: e.target.value })
                      }
                      className='h-8 font-mono'
                    />
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}

        <Button type='button' variant='outline' size='sm' onClick={addRule}>
          <Plus className='size-3.5' aria-hidden='true' />
          {t('Add rule')}
        </Button>
      </div>
    </Dialog>
  )
}
