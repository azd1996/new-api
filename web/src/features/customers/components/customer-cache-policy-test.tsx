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
import { useMutation, useQuery } from '@tanstack/react-query'
import { useFormContext, useWatch } from 'react-hook-form'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { CopyButton } from '@/components/copy-button'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getGroups } from '@/features/users/api'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  testCustomerCacheOptimization,
  type CacheOptimizationTestParams,
  type CacheOptimizationTestResponse,
  type CacheOptimizationTestResult,
} from '../api'
import type { CustomerFormValues } from '../lib/customer-form'
import { diffJsonTrees, formatJsonDiffValue, type JsonDiffChange } from '../lib/json-diff'
import { diffLines, type DiffLine } from '../lib/line-diff'
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

// Default client headers pre-filled in the request-header editor. They mirror
// what Claude Code sends; the user may keep, edit or delete each line. Only
// the full-relay mode sends them as the simulated client headers.
const DEFAULT_REQUEST_HEADERS_TEXT = [
  'Accept: text/event-stream',
  'Anthropic-Beta: context-management-2025-06-27',
  'Anthropic-Version: 2023-06-01',
  'Content-Type: application/json',
].join('\n')

// Parses "Name: value" lines into a headers map. Invalid lines are reported
// so the caller can block the test instead of silently dropping them.
function parseHeaderLines(text: string): {
  headers: Record<string, string>
  invalidLines: string[]
} {
  const headers: Record<string, string> = {}
  const invalidLines: string[] = []
  for (const rawLine of text.split('\n')) {
    const line = rawLine.trim()
    if (!line) continue
    const separatorIndex = line.indexOf(':')
    if (separatorIndex <= 0) {
      invalidLines.push(line)
      continue
    }
    const name = line.slice(0, separatorIndex).trim()
    const value = line.slice(separatorIndex + 1).trim()
    if (!name) {
      invalidLines.push(line)
      continue
    }
    headers[name] = value
  }
  return { headers, invalidLines }
}

type CacheOptimizationTestMode = NonNullable<
  CacheOptimizationTestParams['mode']
>

// Bodies above this size skip the full body / full line diff rendering and
// offer copy/download instead, to keep the drawer responsive.
const LARGE_BODY_BYTES = 1024 * 1024

function formatBody(body: string) {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

function isLargeBody(body: string) {
  return body.length > LARGE_BODY_BYTES
}

type ResultViewProps = {
  item: CacheOptimizationTestResult
  originalBody: string
}

function JsonChangeRow(props: { change: JsonDiffChange }) {
  const { t } = useTranslation()
  const change = props.change
  let typeLabel = t('changed')
  let badgeVariant: 'destructive' | 'outline' | 'secondary' = 'outline'
  let detail: string
  if (change.type === 'added') {
    typeLabel = t('added')
    badgeVariant = 'secondary'
    detail = `→ ${formatJsonDiffValue(change.after)}`
  } else if (change.type === 'removed') {
    typeLabel = t('removed')
    badgeVariant = 'destructive'
    detail = `(${formatJsonDiffValue(change.before)})`
  } else {
    detail = `: ${formatJsonDiffValue(change.before)} → ${formatJsonDiffValue(change.after)}`
  }
  return (
    <div className='text-xs'>
      <Badge variant={badgeVariant} className='mr-1.5'>
        {typeLabel}
      </Badge>
      <span className='font-mono break-all'>{change.path}</span>
      <span className='text-muted-foreground'> {detail}</span>
    </div>
  )
}

function LineDiffView(props: { lines: DiffLine[] }) {
  // Diff lines can legitimately repeat (same type and text), so keys are
  // built from content plus an occurrence counter instead of the array index.
  const keyCounts = new Map<string, number>()
  return (
    <pre className='bg-muted/40 max-h-72 overflow-auto rounded-md p-3 font-mono text-xs break-all whitespace-pre-wrap'>
      {props.lines.map((line) => {
        let prefix = '  '
        let className = ''
        if (line.type === 'added') {
          prefix = '+ '
          className = 'bg-success/15 text-success'
        } else if (line.type === 'removed') {
          prefix = '- '
          className = 'bg-destructive/15 text-destructive'
        }
        const baseKey = `${line.type}-${line.text}`
        const occurrence = keyCounts.get(baseKey) ?? 0
        keyCounts.set(baseKey, occurrence + 1)
        return (
          <div
            key={occurrence === 0 ? baseKey : `${baseKey}-${occurrence}`}
            className={className}
          >
            {prefix}
            {line.text}
          </div>
        )
      })}
    </pre>
  )
}

function ResultView(props: ResultViewProps) {
  const { t } = useTranslation()
  const item = props.item
  const [view, setView] = useState<'changes' | 'result'>('changes')
  const hasChanges = (item.changed_steps?.length ?? 0) > 0
  const large = isLargeBody(item.body ?? '')
  const changes = useMemo(
    () => (large ? [] : diffJsonTrees(props.originalBody, item.body ?? '')),
    [large, props.originalBody, item.body]
  )
  const lineDiff = useMemo(
    () => (large || (changes && changes.length > 50) ? [] : diffLines(formatBody(props.originalBody), formatBody(item.body ?? ''))),
    [large, changes, props.originalBody, item.body]
  )
  const prettyBody = useMemo(() => formatBody(item.body ?? ''), [item.body])

  let body: React.ReactNode
  if (view === 'changes' && !hasChanges) {
    body = <p className='text-muted-foreground text-xs'>{t('No changes')}</p>
  } else if (view === 'changes' && large) {
    body = (
      <p className='text-muted-foreground text-xs'>
        {t(
          'Body exceeds 1MB: only changed steps are listed here. Use copy to inspect the full result.'
        )}
      </p>
    )
  } else if (view === 'changes') {
    body = (
      <div className='space-y-2'>
        {changes && changes.length > 0 ? (
          <div className='space-y-1'>
            {changes.map((change) => (
              <JsonChangeRow
                key={`${change.type}-${change.path}`}
                change={change}
              />
            ))}
          </div>
        ) : (
          <p className='text-muted-foreground text-xs'>
            {t('The step ran, but the body is identical to the input.')}
          </p>
        )}
        {lineDiff.length > 0 && (
          <details>
            <summary className='text-muted-foreground cursor-pointer text-xs'>
              {t('Full diff')}
            </summary>
            <div className='mt-2'>
              <LineDiffView lines={lineDiff} />
            </div>
          </details>
        )}
      </div>
    )
  } else if (large) {
    body = (
      <p className='text-muted-foreground text-xs'>
        {t('Body exceeds 1MB: the full result is not rendered. Use copy to inspect it.')}
      </p>
    )
  } else {
    body = (
      <pre className='bg-muted/40 max-h-72 overflow-auto rounded-md p-3 font-mono text-xs break-all whitespace-pre-wrap'>
        {prettyBody}
      </pre>
    )
  }

  return (
    <Tabs value={view} onValueChange={(value) => setView(value as 'changes' | 'result')}>
      <div className='flex items-center justify-between gap-2'>
        <TabsList>
          <TabsTrigger value='changes' disabled={!hasChanges}>
            {t('Changes')}
          </TabsTrigger>
          <TabsTrigger value='result'>{t('Result')}</TabsTrigger>
        </TabsList>
        <CopyButton value={prettyBody} tooltip={t('Copy result')} />
      </div>
      {body}
    </Tabs>
  )
}

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
  const [headersText, setHeadersText] = useState(
    DEFAULT_REQUEST_HEADERS_TEXT
  )
  const [group, setGroup] = useState('default')
  const [mode, setMode] = useState<CacheOptimizationTestMode>(
    'cache_optimization'
  )
  const [result, setResult] = useState<CacheOptimizationTestResponse | null>(
    null
  )
  const [originalBodies, setOriginalBodies] = useState<string[]>([])

  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: async () => requireServerSuccess(await getGroups()),
  })
  const groupOptions = useMemo(() => {
    // The default group may not exist as a configured ratio yet; always keep
    // it selectable so the preview's default value stays valid.
    return [...new Set([...(groupsData?.data ?? []), 'default'])]
  }, [groupsData])

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
    let headers: Record<string, string> | undefined
    if (mode === 'full_relay') {
      const parsed = parseHeaderLines(headersText)
      if (parsed.invalidLines.length > 0) {
        toast.error(
          t('Invalid header lines (expected "Name: value")', {
            lines: parsed.invalidLines.join(', '),
          })
        )
        return
      }
      headers = Object.keys(parsed.headers).length > 0 ? parsed.headers : undefined
    }
    setResult(null)
    setOriginalBodies(bodies)
    testMutation.mutate({
      customer_id: props.customerId,
      config: policy ?? undefined,
      group,
      mode,
      items: bodies.map((body) => ({ body, headers })),
    })
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
      {mode === 'full_relay' && (
        <div className='space-y-1'>
          <Label htmlFor='cache-optimization-test-headers'>
            {t('Simulated client headers')}
          </Label>
          <Textarea
            id='cache-optimization-test-headers'
            value={headersText}
            onChange={(event) => setHeadersText(event.target.value)}
            rows={5}
            placeholder='Anthropic-Version: 2023-06-01'
            aria-label={t('Simulated client headers')}
            className='font-mono text-xs'
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'One "Name: value" per line. These headers act as the client request headers in the full-relay preview; edit or delete lines freely.'
            )}
          </p>
        </div>
      )}
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
          <Select
            value={group}
            onValueChange={(value) => setGroup(value ?? 'default')}
          >
            <SelectTrigger
              id='cache-optimization-test-group'
              className='w-56'
              title={group}
            >
              <SelectValue className='min-w-0'>
                <span className='truncate'>{group}</span>
              </SelectValue>
            </SelectTrigger>
            <SelectContent
              alignItemWithTrigger={false}
              className='w-72 max-w-[calc(100vw-2rem)]'
            >
              {groupOptions.map((option) => (
                <Tooltip key={option}>
                  <TooltipTrigger
                    render={
                      <SelectItem
                        value={option}
                        className='max-w-full'
                        title={option}
                      />
                    }
                  >
                    <span className='block truncate'>{option}</span>
                  </TooltipTrigger>
                  <TooltipContent
                    align='start'
                    className='max-w-xs break-all whitespace-normal'
                  >
                    {option}
                  </TooltipContent>
                </Tooltip>
              ))}
            </SelectContent>
          </Select>
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
                      {t(
                        mode === 'full_relay'
                          ? 'Outbound headers (redacted)'
                          : 'Headers to add'
                      )}
                      :{' '}
                      {Object.entries(item.headers)
                        .map(([key, value]) => `${key}: ${value}`)
                        .join(', ')}
                    </div>
                  )}
                  <ResultView
                    item={item}
                    originalBody={originalBodies[item.index] ?? ''}
                  />
                </>
              )}
            </div>
          ))}
        </div>
      )}
    </SideDrawerSection>
  )
}
