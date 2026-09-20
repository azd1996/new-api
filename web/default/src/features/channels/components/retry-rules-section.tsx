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
// ({status_code, error_message}); operations rewrite the request body. Each
// operation may set "action": "retry_same_channel" (default) or
// "fallback_next_channel" to hand off to the next channel instead.
const THINKING_TEMPLATE = JSON.stringify(
  {
    operations: [
      {
        mode: 'prune_objects',
        path: 'messages',
        value: { where: { type: 'thinking' } },
        conditions: [
          { path: 'status_code', mode: 'full', value: 400 },
          { path: 'error_message', mode: 'contains', value: 'thinking' },
        ],
        logic: 'AND',
        action: 'retry_same_channel',
      },
      {
        mode: 'prune_objects',
        path: 'messages',
        value: { where: { type: 'redacted_thinking' } },
        conditions: [
          { path: 'status_code', mode: 'full', value: 400 },
          { path: 'error_message', mode: 'contains', value: 'thinking' },
        ],
        logic: 'AND',
        action: 'retry_same_channel',
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
              'On an upstream error, match the response (status code / error message) with each operation conditions; matching operations rewrite the request body. Set each operation action to "retry_same_channel" (default) to retry the rewritten request once on the same channel, or "fallback_next_channel" to hand it off to the next channel (requires RetryTimes > 0 and another available channel). Leave empty to disable.'
            )}
          </p>
          <p className='text-sm text-muted-foreground'>
            {t(
              'To catch an error returned inside a 200 response body (e.g. a rate-limit message), add a "response_body" condition (with status_code 200); the request is then retried on the next channel (200-body matches always fall back). For streaming, the offending chunk is dropped and the next channel continues the response.'
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
          'Retry override operations as JSON, e.g. {"operations": [...]}. Leave empty to disable.'
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

