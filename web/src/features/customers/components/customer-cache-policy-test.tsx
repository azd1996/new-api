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
import { useMutation } from '@tanstack/react-query'
import { useFormContext, useWatch } from 'react-hook-form'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import {
  testCustomerCacheOptimization,
  type CacheOptimizationTestParams,
  type CacheOptimizationTestResponse,
} from '../api'
import type { CustomerFormValues } from '../lib/customer-form'
import { splitJsonBodies } from '../lib/session-identity'

const ruleLabels: Record<string, string> = {
  force_extended_ttl: 'Extended cache TTL',
  fix_volatile_breakpoint: 'Fix volatile breakpoint',
  retain_previous_breakpoint: 'Retain previous breakpoint',
  stable_base_breakpoint: 'Stable base breakpoint',
  fallback_cache_breakpoint: 'Fallback cache breakpoint',
}

const stepLabels: Record<string, string> = {
  ...ruleLabels,
  clear_at_cleanup: 'Removed cache_control from turn-scoped messages',
}

const skippedStepLabels: Record<string, string> = {
  retain_previous_breakpoint:
    'Retain previous breakpoint (requires upstream cache state)',
}

type CacheOptimizationTestMode = NonNullable<
  CacheOptimizationTestParams['mode']
>

type CustomerCachePolicyTestProps = {
  customerId?: string
}

export function CustomerCachePolicyTest(props: CustomerCachePolicyTestProps) {
  const { t } = useTranslation()
  const form = useFormContext<CustomerFormValues>()
  const policy = useWatch({
    control: form.control,
    name: 'claude_cache_optimization',
  })
  const [requestsText, setRequestsText] = useState('')
  const [group, setGroup] = useState('default')
  const [mode, setMode] = useState<CacheOptimizationTestMode>(
    'cache_optimization'
  )
  const [result, setResult] = useState<CacheOptimizationTestResponse | null>(
    null
  )

  const testMutation = useMutation({
    mutationFn: testCustomerCacheOptimization,
    onSuccess: setResult,
    onError: (error: unknown) =>
      handleServerError(error, t('Failed to test cache optimization')),
  })

  const handleTest = () => {
    const bodies = splitJsonBodies(requestsText)
    if (bodies.length === 0) {
      toast.error(t('Paste at least one valid JSON request body'))
      return
    }
    setResult(null)
    testMutation.mutate({
      customer_id: props.customerId,
      config: policy ?? undefined,
      group,
      mode,
      items: bodies.map((body) => ({ body })),
    })
  }

  const formatBody = (body: string) => {
    try {
      return JSON.stringify(JSON.parse(body), null, 2)
    } catch {
      return body
    }
  }

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Cache optimization test')}
        description={t(
          'Preview the outbound request body for pasted Claude requests. Choose cache-only to run the optimization pipeline directly, or full relay to also run channel selection, model mapping, protocol conversion and overrides — nothing is sent upstream.'
        )}
      />
      <p className='text-muted-foreground text-xs'>
        {t(
          'The retain-previous-breakpoint rule depends on upstream cache state and cannot be previewed here.'
        )}
      </p>
      <Textarea
        value={requestsText}
        onChange={(event) => setRequestsText(event.target.value)}
        rows={5}
        placeholder='{"model":"claude-opus-4-6","system":[...],"messages":[...]}'
        aria-label={t('Request bodies')}
        className='font-mono text-xs'
      />
      <p className='text-muted-foreground text-xs'>
        {t(
          'Paste minified JSON one per line or pretty-printed objects back to back.'
        )}
      </p>
      <div className='flex flex-wrap items-end gap-3'>
        <div className='space-y-1'>
          <Label htmlFor='cache-optimization-test-mode'>
            {t('Test mode')}
          </Label>
          <NativeSelect
            id='cache-optimization-test-mode'
            value={mode}
            onChange={(event) =>
              setMode(event.target.value as CacheOptimizationTestMode)
            }
            className='w-56'
          >
            <option value='cache_optimization'>
              {t('Cache optimizations only')}
            </option>
            <option value='full_relay'>{t('Full relay (no upstream call)')}</option>
          </NativeSelect>
        </div>
        <div className='space-y-1'>
          <Label htmlFor='cache-optimization-test-group'>{t('Group')}</Label>
          <Input
            id='cache-optimization-test-group'
            value={group}
            onChange={(event) => setGroup(event.target.value)}
            className='w-40'
          />
        </div>
        <Button
          type='button'
          variant='outline'
          onClick={handleTest}
          disabled={testMutation.isPending}
        >
          {testMutation.isPending ? t('Testing...') : t('Test optimizations')}
        </Button>
      </div>
      {result && (
        <div className='space-y-3'>
          {result.results.map((item) => (
            <div key={item.index} className='space-y-2 rounded-md border p-3'>
              <div className='flex items-center justify-between gap-2'>
                <span className='text-sm font-medium'>
                  #{item.index + 1}
                  {item.model ? ` · ${item.model}` : ''}
                </span>
                {!item.ok && (
                  <span className='text-destructive text-xs'>{item.error}</span>
                )}
              </div>
              {item.ok && (
                <>
                  {mode === 'full_relay' && (
                    <div className='text-muted-foreground grid gap-x-4 gap-y-1 text-xs sm:grid-cols-2'>
                      <span>
                        {t('Channel')}: {item.channel_name} (#{item.channel_id})
                      </span>
                      <span>
                        {t('Group')}: {item.group}
                      </span>
                      <span>
                        {t('Model')}:{' '}
                        {item.upstream_model &&
                        item.upstream_model !== item.origin_model
                          ? `${item.origin_model} → ${item.upstream_model}`
                          : (item.origin_model ?? '')}
                        {item.model_mapped ? ` (${t('mapped')})` : ''}
                      </span>
                      <span className='truncate' title={item.url}>
                        {t('Upstream URL')}: {item.method} {item.url}
                      </span>
                    </div>
                  )}
                  {item.pass_through && (
                    <p className='text-warning text-xs'>
                      {t(
                        'Pass-through mode is enabled: the request body is forwarded unmodified and cache optimizations are skipped.'
                      )}
                    </p>
                  )}
                  <div className='flex flex-wrap items-center gap-1.5'>
                    {(item.enabled_rules ?? []).map((rule) => (
                      <Badge key={rule} variant='secondary'>
                        {t(ruleLabels[rule] ?? rule)}
                      </Badge>
                    ))}
                    {(item.changed_steps ?? []).map((step) => (
                      <Badge key={step} variant='outline'>
                        {t(stepLabels[step] ?? step)}
                      </Badge>
                    ))}
                    {(item.skipped_steps ?? []).map((step) => (
                      <Badge key={step} variant='warning'>
                        {t(skippedStepLabels[step] ?? step)}
                      </Badge>
                    ))}
                    {(item.changed_steps?.length ?? 0) === 0 &&
                      (item.skipped_steps?.length ?? 0) === 0 && (
                        <span className='text-muted-foreground text-xs'>
                          {t('No changes')}
                        </span>
                      )}
                  </div>
                  {item.headers && Object.keys(item.headers).length > 0 && (
                    <div className='text-muted-foreground text-xs'>
                      {t('Headers to add')}:{' '}
                      {Object.entries(item.headers)
                        .map(([key, value]) => `${key}: ${value}`)
                        .join(', ')}
                    </div>
                  )}
                  <div className='relative'>
                    <pre className='bg-muted/40 max-h-72 overflow-auto rounded-md p-3 font-mono text-xs break-all whitespace-pre-wrap'>
                      {formatBody(item.body ?? '')}
                    </pre>
                    <div className='absolute top-2 right-2'>
                      <CopyButton
                        value={formatBody(item.body ?? '')}
                        tooltip={t('Copy result')}
                      />
                    </div>
                  </div>
                </>
              )}
            </div>
          ))}
        </div>
      )}
    </SideDrawerSection>
  )
}
