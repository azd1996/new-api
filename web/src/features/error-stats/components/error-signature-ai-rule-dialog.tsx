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
import { Plus, RefreshCw, Sparkles } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import type { AIGeneratedRule, ErrorStatItem, SignatureRule } from '../types'

interface ErrorSignatureAIRuleDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  item: ErrorStatItem | null
  generating: boolean
  result: AIGeneratedRule | null
  error: string | null
  attributionOptions: string[]
  onRegenerate: () => void
  onAddRule: (rule: SignatureRule) => void
}

export function ErrorSignatureAIRuleDialog(
  props: ErrorSignatureAIRuleDialogProps
) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<SignatureRule | null>(null)

  // Reset the editable copy whenever a fresh AI result arrives.
  useEffect(() => {
    setDraft(props.result ? { ...props.result.rule } : null)
  }, [props.result])

  const samples = props.result?.samples ?? []
  const attributionOptions: ComboboxInputOption[] =
    props.attributionOptions.map((value) => ({ value, label: value }))

  // Re-check the (possibly edited) pattern against the raw samples in the
  // browser. Go RE2 and JS regex differ slightly, so an uncompilable-in-JS
  // pattern falls back to the backend's verdict instead of a false alarm.
  let browserCheck: boolean | null = null
  if (draft && samples.length > 0) {
    try {
      const re = new RegExp(draft.pattern)
      browserCheck = samples.some((sample) => re.test(sample))
    } catch {
      browserCheck = null
    }
  }

  const matched = browserCheck ?? props.result?.matched_sample ?? false

  const updateDraft = (patch: Partial<SignatureRule>) =>
    setDraft((prev) => (prev ? { ...prev, ...patch } : prev))

  const canAdd =
    draft !== null &&
    draft.name.trim() !== '' &&
    draft.pattern.trim() !== '' &&
    draft.signature.trim() !== ''

  const handleAdd = () => {
    if (!draft) return
    props.onAddRule(draft)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('AI-generated rule')}
      description={t(
        'Review the rule, adjust it if needed, then add it to the normalization rules. It takes effect when the rule editor is saved.'
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
            variant='outline'
            size='sm'
            onClick={props.onRegenerate}
            disabled={props.generating || !props.item}
          >
            <RefreshCw
              className={props.generating ? 'animate-spin' : ''}
              aria-hidden='true'
            />
            {props.generating ? t('Generating...') : t('Regenerate')}
          </Button>
          <Button
            type='button'
            size='sm'
            onClick={handleAdd}
            disabled={props.generating || !canAdd}
          >
            <Plus className='size-3.5' aria-hidden='true' />
            {t('Add to rules')}
          </Button>
        </>
      }
    >
      <div className='space-y-3'>
        {props.item && (
          <div className='bg-muted/50 space-y-1 rounded-md border p-2'>
            <span className='text-muted-foreground text-xs'>
              {t('Detailed Error')}
            </span>
            <p className='font-mono text-xs break-all'>
              {props.item.signature}
            </p>
          </div>
        )}

        {props.generating && (
          <div className='text-muted-foreground flex flex-col items-center gap-2 py-8 text-sm'>
            <Sparkles className='size-5 animate-pulse' aria-hidden='true' />
            {t('Asking the AI assistant...')}
          </div>
        )}

        {!props.generating && props.error && (
          <div className='text-destructive bg-destructive/10 rounded-md border p-3 text-sm break-all'>
            {props.error}
          </div>
        )}

        {!props.generating && draft && (
          <>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label
                  htmlFor='ai-rule-name'
                  className='text-muted-foreground text-xs'
                >
                  {t('Rule name')}
                </Label>
                <Input
                  id='ai-rule-name'
                  value={draft.name}
                  onChange={(e) => updateDraft({ name: e.target.value })}
                  className='h-8'
                />
              </div>
              <div className='space-y-1.5'>
                <Label
                  htmlFor='ai-rule-attribution'
                  className='text-muted-foreground text-xs'
                >
                  {t('Error Attribution')}
                </Label>
                <Combobox
                  id='ai-rule-attribution'
                  options={attributionOptions}
                  value={draft.attribution}
                  onValueChange={(value) =>
                    updateDraft({ attribution: value ?? '' })
                  }
                  placeholder={t('Error Attribution')}
                  searchPlaceholder={t('Error Attribution')}
                  emptyText={t('No option found.')}
                  className='h-8'
                />
              </div>
            </div>

            <div className='space-y-1.5'>
              <Label
                htmlFor='ai-rule-pattern'
                className='text-muted-foreground text-xs'
              >
                {t('Pattern')}
              </Label>
              <Input
                id='ai-rule-pattern'
                value={draft.pattern}
                onChange={(e) => updateDraft({ pattern: e.target.value })}
                className='font-mono'
              />
            </div>

            <div className='space-y-1.5'>
              <Label
                htmlFor='ai-rule-signature'
                className='text-muted-foreground text-xs'
              >
                {t('Normalized error signature')}
              </Label>
              <Input
                id='ai-rule-signature'
                value={draft.signature}
                onChange={(e) => updateDraft({ signature: e.target.value })}
                className='font-mono'
              />
            </div>

            <div className='space-y-1.5'>
              <Label
                htmlFor='ai-rule-note'
                className='text-muted-foreground text-xs'
              >
                {t('Note')}
              </Label>
              <Textarea
                id='ai-rule-note'
                value={draft.note}
                onChange={(e) => updateDraft({ note: e.target.value })}
                rows={2}
              />
            </div>

            <div className='flex items-center gap-2'>
              <Switch
                checked={draft.enabled}
                onCheckedChange={(checked) => updateDraft({ enabled: checked })}
                aria-label={t('Enabled')}
              />
              <span className='text-muted-foreground text-xs'>
                {t('Enabled')}
              </span>
              {samples.length > 0 && (
                <Badge
                  variant={matched ? 'secondary' : 'destructive'}
                  className='ml-auto'
                >
                  {matched
                    ? t('Pattern matches the sample error.')
                    : t('Pattern does not match the sample error.')}
                </Badge>
              )}
            </div>

            <div className='space-y-1'>
              <span className='text-muted-foreground text-xs'>
                {t('Raw error samples used by the AI')}
              </span>
              <pre className='bg-background max-h-40 overflow-auto rounded-md border p-2 font-mono text-xs break-all whitespace-pre-wrap'>
                {samples.join('\n\n---\n\n')}
              </pre>
            </div>
          </>
        )}
      </div>
    </Dialog>
  )
}
