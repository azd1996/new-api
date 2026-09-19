import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { Plus, Trash2, Wand2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import type { ChannelFormValues } from '../lib/channel-form'
import type { RetryRule } from '../types'
import { ParamOverrideEditorDialog } from './dialogs/param-override-editor-dialog'

type RetryRulesSectionProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled?: boolean
}

function parseRules(value: string | undefined): RetryRule[] {
  if (!value || value.trim() === '') return []
  try {
    const parsed = JSON.parse(value)
    return Array.isArray(parsed) ? (parsed as RetryRule[]) : []
  } catch {
    return []
  }
}

export function RetryRulesSection(props: RetryRulesSectionProps) {
  const { t } = useTranslation()
  const [transformEditorIndex, setTransformEditorIndex] = useState<number | null>(
    null
  )

  const fallbackEnabled = props.form.watch('thinking_fallback_enabled') || false
  const rulesJson = props.form.watch('retry_rules')
  const rules = useMemo(() => parseRules(rulesJson), [rulesJson])

  const commitRules = (next: RetryRule[]) => {
    props.form.setValue(
      'retry_rules',
      next.length > 0 ? JSON.stringify(next) : '',
      { shouldDirty: true }
    )
  }

  const updateRule = (index: number, patch: Partial<RetryRule>) => {
    commitRules(rules.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)))
  }

  const updateMatch = (index: number, patch: Partial<RetryRule['match']>) => {
    updateRule(index, { match: { ...(rules[index]?.match ?? {}), ...patch } })
  }

  const updateRetry = (
    index: number,
    patch: Partial<NonNullable<RetryRule['retry']>>
  ) => {
    updateRule(index, { retry: { ...(rules[index]?.retry ?? {}), ...patch } })
  }

  const addRule = () => {
    commitRules([
      ...rules,
      {
        name: '',
        match: { status_codes: [400] },
        transform: [],
        retry: { target: 'original_channel', max_attempts: 1 },
      },
    ])
  }

  const removeRule = (index: number) => {
    commitRules(rules.filter((_, i) => i !== index))
  }

  // RETRY_RULES_SECTION_RENDER_PLACEHOLDER
  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between px-4 py-3'>
        <div className='space-y-0.5'>
          <span className='text-sm font-medium'>
            {t('Thinking Fallback Retry')}
          </span>
          <p className='text-sm text-muted-foreground'>
            {t(
              'On thinking-related 400 errors, strip thinking blocks and retry on the same channel'
            )}
          </p>
        </div>
        <Switch
          checked={fallbackEnabled}
          disabled={props.disabled}
          onCheckedChange={(checked) =>
            props.form.setValue('thinking_fallback_enabled', checked, {
              shouldDirty: true,
            })
          }
        />
      </div>

      <div className='space-y-3 px-4 pb-3'>
        <div className='flex items-center justify-between'>
          <span className='text-sm font-medium'>{t('Custom Retry Rules')}</span>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={addRule}
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add Rule')}
          </Button>
        </div>
        {rules.length === 0 && (
          <p className='text-sm text-muted-foreground'>
            {t(
              'No custom rules. When enabled above, built-in thinking rules apply.'
            )}
          </p>
        )}
        {/* RETRY_RULES_SECTION_ROWS_PLACEHOLDER */}
        {rules.map((rule, index) => (
          <div key={index} className='space-y-2 rounded-md border p-3'>
            <div className='flex items-center gap-2'>
              <Input
                placeholder={t('Rule name')}
                value={rule.name ?? ''}
                disabled={props.disabled}
                onChange={(e) => updateRule(index, { name: e.target.value })}
              />
              <Button
                type='button'
                variant='ghost'
                size='icon'
                disabled={props.disabled}
                onClick={() => removeRule(index)}
                aria-label={t('Remove')}
              >
                <Trash2 className='h-4 w-4' />
              </Button>
            </div>
            <Input
              placeholder={t('Status codes (comma separated), e.g. 400')}
              value={(rule.match?.status_codes ?? []).join(', ')}
              disabled={props.disabled}
              onChange={(e) =>
                updateMatch(index, {
                  status_codes: e.target.value
                    .split(',')
                    .map((s) => parseInt(s.trim(), 10))
                    .filter((n) => !Number.isNaN(n)),
                })
              }
            />
            <Input
              placeholder={t('Error message regex')}
              value={rule.match?.error_regex ?? ''}
              disabled={props.disabled}
              onChange={(e) => updateMatch(index, { error_regex: e.target.value })}
            />
            <Input
              placeholder={t('Relay format (optional), e.g. claude')}
              value={rule.match?.relay_format ?? ''}
              disabled={props.disabled}
              onChange={(e) =>
                updateMatch(index, { relay_format: e.target.value })
              }
            />
            <div className='flex items-center gap-2'>
              <Input
                type='number'
                placeholder={t('Max attempts')}
                value={rule.retry?.max_attempts ?? 1}
                disabled={props.disabled}
                onChange={(e) =>
                  updateRetry(index, {
                    max_attempts: parseInt(e.target.value, 10) || 0,
                  })
                }
              />
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={props.disabled}
                onClick={() => setTransformEditorIndex(index)}
              >
                <Wand2 className='mr-2 h-4 w-4' />
                {t('Edit transform')}
              </Button>
            </div>
          </div>
        ))}
      </div>
      {/* RETRY_RULES_SECTION_DIALOG_PLACEHOLDER */}
      {transformEditorIndex !== null && !props.disabled && (
        <ParamOverrideEditorDialog
          open={transformEditorIndex !== null}
          value={JSON.stringify(
            { operations: rules[transformEditorIndex]?.transform ?? [] },
            null,
            2
          )}
          onOpenChange={(open) => {
            if (!open) setTransformEditorIndex(null)
          }}
          onSave={(nextValue) => {
            if (transformEditorIndex === null) return
            let ops: Array<Record<string, unknown>> = []
            try {
              const parsed = JSON.parse(nextValue)
              if (Array.isArray(parsed?.operations)) {
                ops = parsed.operations
              }
            } catch {
              ops = []
            }
            updateRule(transformEditorIndex, { transform: ops })
          }}
        />
      )}
    </div>
  )
}


