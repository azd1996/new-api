import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { Code, Wand2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

import type { ChannelFormValues } from '../lib/channel-form'
import { ParamOverrideEditorDialog } from './dialogs/param-override-editor-dialog'

type RetryRulesSectionProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled?: boolean
}

// Sample rule: strip thinking / redacted_thinking blocks, gated on a 400 whose
// error message mentions "thinking". Conditions match the response context
// ({status_code, error_message}); operations rewrite the request body.
const THINKING_TEMPLATE = JSON.stringify(
  {
    operations: [
      {
        mode: 'prune_objects',
        path: 'messages.#.content',
        value: { where: { type: 'thinking' } },
        conditions: [
          { path: 'status_code', mode: 'full', value: 400 },
          { path: 'error_message', mode: 'contains', value: 'thinking' },
        ],
        logic: 'AND',
      },
      {
        mode: 'prune_objects',
        path: 'messages.#.content',
        value: { where: { type: 'redacted_thinking' } },
        conditions: [
          { path: 'status_code', mode: 'full', value: 400 },
          { path: 'error_message', mode: 'contains', value: 'thinking' },
        ],
        logic: 'AND',
      },
    ],
  },
  null,
  2
)

export function RetryRulesSection(props: RetryRulesSectionProps) {
  const { t } = useTranslation()
  const [editorOpen, setEditorOpen] = useState(false)

  const value = props.form.watch('retry_override') || ''

  const setValue = (next: string) => {
    props.form.setValue('retry_override', next, { shouldDirty: true })
  }

  return (
    <div className='space-y-3 border-t pt-4'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
        <div className='space-y-1'>
          <span className='text-sm font-medium'>
            {t('Retry Override Rules')}
          </span>
          <p className='text-sm text-muted-foreground'>
            {t(
              'On an upstream error, match the response (status code / error message) with each operation conditions; matching operations rewrite the request body and the request is retried once on the same channel. Leave empty to disable.'
            )}
          </p>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => setEditorOpen(true)}
          >
            <Wand2 className='mr-2 h-4 w-4' />
            {t('Visual edit')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => setValue(THINKING_TEMPLATE)}
          >
            <Code className='mr-2 h-4 w-4' />
            {t('Fill Template')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            disabled={props.disabled}
            onClick={() => setValue('')}
          >
            {t('Clear')}
          </Button>
        </div>
      </div>
      <Textarea
        value={value}
        onChange={(e) => setValue(e.target.value)}
        disabled={props.disabled}
        rows={8}
        placeholder={t(
          'On an upstream error, match the response (status code / error message) with each operation conditions; matching operations rewrite the request body and the request is retried once on the same channel. Leave empty to disable.'
        )}
        className='max-h-72 min-h-40 resize-y overflow-auto font-mono text-xs'
      />

      {editorOpen && !props.disabled && (
        <ParamOverrideEditorDialog
          open={editorOpen}
          value={value}
          onOpenChange={setEditorOpen}
          onSave={setValue}
        />
      )}
    </div>
  )
}

