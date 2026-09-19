import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { Code, Wand2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import type { ChannelFormValues } from '../lib/channel-form'
import { ParamOverrideEditorDialog } from './dialogs/param-override-editor-dialog'

type RetryRulesSectionProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled?: boolean
}

// Default rewrite: strip thinking / redacted_thinking blocks. Same
// {operations:[...]} shape as param_override, so it reuses that editor verbatim.
const DEFAULT_THINKING_TRANSFORM = JSON.stringify(
  {
    operations: [
      {
        mode: 'prune_objects',
        path: 'messages.#.content',
        value: { where: { type: 'thinking' } },
      },
      {
        mode: 'prune_objects',
        path: 'messages.#.content',
        value: { where: { type: 'redacted_thinking' } },
      },
    ],
  },
  null,
  2
)

export function RetryRulesSection(props: RetryRulesSectionProps) {
  const { t } = useTranslation()
  const [editorOpen, setEditorOpen] = useState(false)

  const enabled = props.form.watch('thinking_fallback_enabled') || false
  const transform = props.form.watch('thinking_fallback_transform') || ''

  const setTransform = (value: string) => {
    props.form.setValue('thinking_fallback_transform', value, {
      shouldDirty: true,
    })
  }

  return (
    <div className='space-y-3 border-t pt-4'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
        <div className='space-y-1'>
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
          checked={enabled}
          disabled={props.disabled}
          onCheckedChange={(checked) => {
            props.form.setValue('thinking_fallback_enabled', checked, {
              shouldDirty: true,
            })
            // On enable, seed the editor with the default thinking rule so it is
            // visible and editable right away (and doubles as a usable sample).
            if (
              checked &&
              (props.form.getValues('thinking_fallback_transform') || '').trim() ===
                ''
            ) {
              setTransform(DEFAULT_THINKING_TRANSFORM)
            }
          }}
        />
      </div>

      {enabled && (
        <div className='space-y-3'>
          <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
            <div className='space-y-1'>
              <span className='text-sm font-medium'>
                {t('Fallback Rewrite')}
              </span>
              <p className='text-sm text-muted-foreground'>
                {t(
                  'Rewrite applied before retrying. Leave empty to strip thinking blocks by default.'
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
                onClick={() => setTransform(DEFAULT_THINKING_TRANSFORM)}
              >
                <Code className='mr-2 h-4 w-4' />
                {t('Fill Template')}
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                disabled={props.disabled}
                onClick={() => setTransform('')}
              >
                {t('Clear')}
              </Button>
            </div>
          </div>
          <Textarea
            value={transform}
            onChange={(e) => setTransform(e.target.value)}
            disabled={props.disabled}
            rows={8}
            placeholder={t(
              'Rewrite applied before retrying. Leave empty to strip thinking blocks by default.'
            )}
            className='max-h-72 min-h-40 resize-y overflow-auto font-mono text-xs'
          />
        </div>
      )}

      {editorOpen && !props.disabled && (
        <ParamOverrideEditorDialog
          open={editorOpen}
          value={transform || ''}
          onOpenChange={setEditorOpen}
          onSave={setTransform}
        />
      )}
    </div>
  )
}

