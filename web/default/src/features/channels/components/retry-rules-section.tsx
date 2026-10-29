import { useTranslation } from 'react-i18next'
import type { UseFormReturn } from 'react-hook-form'
import { Code } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

import type { ChannelFormValues } from '../lib/channel-form'

type RetryRulesSectionProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled?: boolean
}

// Sample four-phase rule: on a Claude 400 whose error message mentions
// "thinking", strip thinking / redacted_thinking blocks from the request and
// retry the same channel. phase1 filters the request (here: any request),
// phase2 matches the response, phase3 rewrites the body, phase4 picks the
// action. phase4_retry_action also accepts the object form
// {"action":"return_error","status_code":429,"status_msg":"..."} to stop
// retrying and return a fixed error to the caller.
const RETRY_RULE_TEMPLATE = JSON.stringify(
  [
    {
      description: 'Strip thinking blocks on a thinking-related 400, then retry',
      phase2_response_conditions: {
        conditions: [
          { path: 'status_code', mode: 'full', value: 400 },
          { path: 'error_message', mode: 'contains', value: 'thinking' },
        ],
        logic: 'AND',
      },
      phase3_request_rewrite: [
        {
          mode: 'prune_objects',
          path: 'messages',
          value: { where: { type: 'thinking' } },
        },
        {
          mode: 'prune_objects',
          path: 'messages',
          value: { where: { type: 'redacted_thinking' } },
        },
      ],
      phase4_retry_action: 'retry_same_channel',
    },
    {
      description: 'Stop retrying on a hard policy block and return 451',
      phase2_response_conditions: {
        conditions: [
          { path: 'status_code', mode: 'full', value: 403 },
          { path: 'error_message', mode: 'contains', value: 'policy' },
        ],
        logic: 'AND',
      },
      phase4_retry_action: {
        action: 'return_error',
        status_code: 451,
        status_msg: 'Blocked by policy',
      },
    },
  ],
  null,
  2
)

export function RetryRulesSection(props: RetryRulesSectionProps) {
  const { t } = useTranslation()

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
              'A JSON array of priority-ordered four-phase rules. The FIRST rule whose phase1_request_condition (model / relay_format / group) and phase2_response_conditions (status_code / error_message / response_body) both match fully decides the outcome: its phase3_request_rewrite rewrites the request body and its phase4_retry_action runs. phase4_retry_action is "retry_same_channel" (default), "fallback_next_channel", or the object form {"action":"return_error","status_code":429,"status_msg":"..."} to stop retrying and return a fixed error to the caller. All phase conditions use the same syntax as parameter override, and omitting "logic" defaults to OR. Leave empty to disable.'
            )}
          </p>
          <p className='text-sm text-muted-foreground'>
            {t(
              'To catch an error returned inside a 200 response body (e.g. a rate-limit message), add a phase2 "response_body" condition with status_code 200; such 200-body matches honor phase4_retry_action (default "retry_same_channel"). "fallback_next_channel" requires RetryTimes > 0 and another available channel; "return_error" stops retrying immediately and returns the configured status_code (default 429).'
            )}
          </p>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() => setValue(RETRY_RULE_TEMPLATE)}
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
          'Retry override rules as a JSON array, e.g. [{ "phase2_response_conditions": {...}, "phase3_request_rewrite": [...], "phase4_retry_action": "retry_same_channel" }]. Leave empty to disable.'
        )}
        className='max-h-72 min-h-40 resize-y overflow-auto font-mono text-xs'
      />
    </div>
  )
}
