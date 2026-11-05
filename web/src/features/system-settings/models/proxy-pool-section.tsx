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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { Lock, Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { getProxyHealth } from '../api'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type {
  ProxyHealthEntry,
  ProxyPoolEntry,
  ProxyTtftRoutingConfig,
  ProxyType,
} from '../types'

const PROXY_SCHEME_REGEX = /^(https?|socks5h?):\/\//i

const PROXY_TYPES: ProxyType[] = ['normal', 'tp', 'force_http']

// i18n keys (English source strings) for each proxy forwarding type.
const PROXY_TYPE_LABELS: Record<ProxyType, string> = {
  normal: 'Normal proxy (follow target scheme)',
  tp: 'tp proxy (scheme header)',
  force_http: 'Force HTTP (downgrade https to http)',
}

const proxyPoolSchema = z.object({
  proxies: z
    .array(
      z.object({
        alias: z.string().trim().min(1, 'Alias is required'),
        url: z
          .string()
          .trim()
          .min(1, 'Proxy address is required')
          .regex(
            PROXY_SCHEME_REGEX,
            'Must start with http://, https://, socks5:// or socks5h://'
          ),
        description: z.string(),
        proxy_type: z.enum(['normal', 'tp', 'force_http']),
        auth: z.string(),
        locked: z.boolean(),
      })
    )
    .superRefine((proxies, ctx) => {
      const seen = new Map<string, number>()
      proxies.forEach((proxy, index) => {
        const alias = proxy.alias.trim()
        if (!alias) return
        if (seen.has(alias)) {
          ctx.addIssue({
            code: z.ZodIssueCode.custom,
            message: 'Alias must be unique',
            path: [index, 'alias'],
          })
        } else {
          seen.set(alias, index)
        }
      })
    }),
})

type ProxyPoolFormValues = z.infer<typeof proxyPoolSchema>

const ttftRoutingSchema = z.object({
  threshold_ms: z.coerce.number<number | string>().int().min(1),
  consecutive_bad: z.coerce.number<number | string>().int().min(1),
  min_explore_ratio: z.coerce.number<number | string>().gt(0).lt(1),
  max_divert_ratio: z.coerce.number<number | string>().gt(0).max(1),
  sample_window: z.coerce.number<number | string>().int().min(1),
})

type TtftRoutingFormInput = z.input<typeof ttftRoutingSchema>
type TtftRoutingFormValues = z.infer<typeof ttftRoutingSchema>

const TTFT_ROUTING_DEFAULTS: TtftRoutingFormValues = {
  threshold_ms: 3000,
  consecutive_bad: 3,
  min_explore_ratio: 0.2,
  max_divert_ratio: 0.8,
  sample_window: 50,
}

const seedTtftRouting = (
  cfg: ProxyTtftRoutingConfig | undefined
): TtftRoutingFormValues => ({
  threshold_ms: cfg?.threshold_ms || TTFT_ROUTING_DEFAULTS.threshold_ms,
  consecutive_bad:
    cfg?.consecutive_bad || TTFT_ROUTING_DEFAULTS.consecutive_bad,
  min_explore_ratio:
    cfg?.min_explore_ratio || TTFT_ROUTING_DEFAULTS.min_explore_ratio,
  max_divert_ratio:
    cfg?.max_divert_ratio || TTFT_ROUTING_DEFAULTS.max_divert_ratio,
  sample_window: cfg?.sample_window || TTFT_ROUTING_DEFAULTS.sample_window,
})

// Field metadata for the TTFT routing config inputs (name, i18n label, step).
const TTFT_ROUTING_FIELDS: Array<{
  name: keyof TtftRoutingFormValues
  label: string
  step: string
}> = [
  { name: 'threshold_ms', label: 'Slow TTFT threshold (ms)', step: '1' },
  { name: 'consecutive_bad', label: 'Consecutive slow samples', step: '1' },
  { name: 'min_explore_ratio', label: 'Min explore ratio', step: '0.05' },
  { name: 'max_divert_ratio', label: 'Max divert ratio', step: '0.05' },
  { name: 'sample_window', label: 'Sample window', step: '1' },
]

type ProxyPoolSectionProps = {
  defaultValues: {
    proxies: ProxyPoolEntry[]
    ttftRouting: ProxyTtftRoutingConfig
  }
}

const normalizeProxies = (proxies: ProxyPoolEntry[]): ProxyPoolEntry[] =>
  proxies.map((proxy) => {
    const description = (proxy.description ?? '').trim()
    const auth = (proxy.auth ?? '').trim()
    const proxyType = proxy.proxy_type ?? 'normal'
    const entry: ProxyPoolEntry = {
      alias: proxy.alias.trim(),
      url: proxy.url.trim(),
    }
    if (description) {
      entry.description = description
    }
    if (proxyType !== 'normal') {
      entry.proxy_type = proxyType
    }
    if (auth) {
      entry.auth = auth
    }
    if (proxy.locked) {
      entry.locked = true
    }
    return entry
  })

// seedProxies ensures every form field has a defined value so the controlled
// inputs never flip between controlled/uncontrolled (proxy_type/auth/locked may
// be absent on entries saved before those columns existed).
const seedProxies = (proxies: ProxyPoolEntry[]) =>
  proxies.map((proxy) => ({
    alias: proxy.alias ?? '',
    url: proxy.url ?? '',
    description: proxy.description ?? '',
    proxy_type: proxy.proxy_type ?? 'normal',
    auth: proxy.auth ?? '',
    locked: proxy.locked ?? false,
  }))

export function ProxyPoolSection(props: ProxyPoolSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const baselineRef = useRef<ProxyPoolEntry[]>(
    normalizeProxies(props.defaultValues.proxies)
  )

  const healthQuery = useQuery({
    queryKey: ['proxy-health'],
    queryFn: getProxyHealth,
    refetchInterval: 15000,
  })
  const healthByAlias = useMemo(() => {
    const map: Record<string, ProxyHealthEntry> = {}
    for (const entry of healthQuery.data?.data ?? []) {
      map[entry.alias] = entry
    }
    return map
  }, [healthQuery.data])

  const form = useForm<ProxyPoolFormValues>({
    resolver: zodResolver(proxyPoolSchema),
    defaultValues: { proxies: seedProxies(props.defaultValues.proxies) },
  })

  const proxiesWatch = form.watch('proxies')
  const fieldArray = useFieldArray({ control: form.control, name: 'proxies' })

  // lockIndex drives the irreversible-lock confirmation dialog (null = closed).
  const [lockIndex, setLockIndex] = useState<number | null>(null)

  const confirmLock = () => {
    if (lockIndex === null) return
    form.setValue(`proxies.${lockIndex}.locked`, true, { shouldDirty: true })
    setLockIndex(null)
  }

  useEffect(() => {
    baselineRef.current = normalizeProxies(props.defaultValues.proxies)
    form.reset({ proxies: seedProxies(props.defaultValues.proxies) })
  }, [props.defaultValues, form])

  const onSubmit = async (data: ProxyPoolFormValues) => {
    const normalized = normalizeProxies(data.proxies)
    if (JSON.stringify(normalized) === JSON.stringify(baselineRef.current)) {
      toast.info(t('No changes to save'))
      return
    }
    await updateOption.mutateAsync({
      key: 'proxy_setting.proxies',
      value: JSON.stringify(normalized),
    })
    baselineRef.current = normalized
  }

  const ttftForm = useForm<
    TtftRoutingFormInput,
    unknown,
    TtftRoutingFormValues
  >({
    resolver: zodResolver(ttftRoutingSchema),
    defaultValues: seedTtftRouting(props.defaultValues.ttftRouting),
  })
  useEffect(() => {
    ttftForm.reset(seedTtftRouting(props.defaultValues.ttftRouting))
  }, [props.defaultValues, ttftForm])

  const onSubmitTtft = async (data: TtftRoutingFormValues) => {
    await updateOption.mutateAsync({
      key: 'proxy_setting.ttft_routing',
      value: JSON.stringify(data),
    })
  }

  return (
    <SettingsSection title={t('Proxy Pool')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save proxy pool'
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Define named proxies here, then reference them by alias in channel settings.'
            )}
          </p>

          {fieldArray.fields.length === 0 ? (
            <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
              {t('No proxies configured. Click "Add proxy" to get started.')}
            </div>
          ) : (
            <div className='space-y-3'>
              {fieldArray.fields.map((field, index) => {
                const locked = Boolean(proxiesWatch?.[index]?.locked)
                return (
                  <div
                    key={field.id}
                    className='space-y-2 rounded-md border p-3'
                  >
                    <div className='flex flex-col gap-2 sm:flex-row sm:items-start'>
                      <FormField
                        control={form.control}
                        name={`proxies.${index}.alias`}
                        render={({ field: aliasField }) => (
                          <FormItem className='sm:w-48'>
                            <FormControl>
                              <Input
                                placeholder={t('Alias (e.g. hk-1)')}
                                disabled={locked}
                                {...aliasField}
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`proxies.${index}.url`}
                        render={({ field: urlField }) => (
                          <FormItem className='flex-1'>
                            <FormControl>
                              <Input
                                placeholder={t(
                                  'http://user:pass@host:port or socks5://host:port'
                                )}
                                disabled={locked}
                                {...urlField}
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <div className='flex flex-col gap-0.5 text-xs sm:w-56 sm:pt-2'>
                        {(() => {
                          const rowAlias = proxiesWatch?.[index]?.alias?.trim()
                          const health = rowAlias
                            ? healthByAlias[rowAlias]
                            : undefined
                          const status = health?.status ?? 'unknown'
                          return (
                            <>
                              {status === 'healthy' && (
                                <span className='font-medium text-green-600'>
                                  {t('Healthy')}
                                </span>
                              )}
                              {status === 'unhealthy' && (
                                <span className='text-destructive font-medium'>
                                  {t('Unhealthy')}
                                </span>
                              )}
                              {status === 'unknown' && (
                                <span className='text-muted-foreground font-medium'>
                                  {t('Unknown')}
                                </span>
                              )}
                              <span className='text-muted-foreground'>
                                {health && health.samples > 0
                                  ? `P50/P90/P99: ${Math.round(health.p50_ms)}/${Math.round(health.p90_ms)}/${Math.round(health.p99_ms)} ms`
                                  : t('No samples yet')}
                              </span>
                              {health && health.ttft_samples > 0 && (
                                <span className='text-muted-foreground'>
                                  {t('Avg TTFT')}:{' '}
                                  {Math.round(health.ttft_avg_ms)} ms (
                                  {health.ttft_samples})
                                </span>
                              )}
                              {health && health.token_speed_samples > 0 && (
                                <span className='text-muted-foreground'>
                                  {t('Prompt tokens/s')}:{' '}
                                  {t('{{value}} tokens/s', {
                                    value: Math.round(health.token_speed_avg),
                                  })}{' '}
                                  ({health.token_speed_samples})
                                </span>
                              )}
                            </>
                          )
                        })()}
                      </div>
                      {locked ? (
                        <Badge variant='secondary' className='gap-1'>
                          <Lock className='h-3 w-3' aria-hidden='true' />
                          {t('Locked')}
                        </Badge>
                      ) : (
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          aria-label={t('Lock proxy')}
                          onClick={() => setLockIndex(index)}
                        >
                          <Lock className='h-4 w-4' />
                        </Button>
                      )}
                      {!locked && (
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          aria-label={t('Remove proxy')}
                          onClick={() => fieldArray.remove(index)}
                        >
                          <Trash2 className='h-4 w-4' />
                        </Button>
                      )}
                    </div>
                    <div className='flex flex-col gap-2 sm:flex-row sm:items-start'>
                      <FormField
                        control={form.control}
                        name={`proxies.${index}.proxy_type`}
                        render={({ field: typeField }) => (
                          <FormItem className='sm:w-64'>
                            <FormControl>
                              <NativeSelect
                                className='w-full'
                                disabled={locked}
                                {...typeField}
                              >
                                {PROXY_TYPES.map((type) => (
                                  <NativeSelectOption key={type} value={type}>
                                    {t(PROXY_TYPE_LABELS[type])}
                                  </NativeSelectOption>
                                ))}
                              </NativeSelect>
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`proxies.${index}.auth`}
                        render={({ field: authField }) => (
                          <FormItem className='flex-1'>
                            <FormControl>
                              <Input
                                placeholder={t(
                                  'Auth (X-Tp-Authorization, used by tp proxy)'
                                )}
                                disabled={locked}
                                {...authField}
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </div>
                    <FormField
                      control={form.control}
                      name={`proxies.${index}.description`}
                      render={({ field: descField }) => (
                        <FormItem>
                          <FormControl>
                            <Input
                              placeholder={t(
                                'Description (optional, shown only here)'
                              )}
                              {...descField}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                )
              })}
            </div>
          )}

          <div>
            <Button
              type='button'
              variant='outline'
              onClick={() =>
                fieldArray.append({
                  alias: '',
                  url: '',
                  description: '',
                  proxy_type: 'normal',
                  auth: '',
                  locked: false,
                })
              }
            >
              <Plus className='mr-2 h-4 w-4' />
              {t('Add proxy')}
            </Button>
          </div>
        </SettingsForm>
      </Form>

      <Form {...ttftForm}>
        <SettingsForm onSubmit={ttftForm.handleSubmit(onSubmitTtft)}>
          <div className='border-t pt-4'>
            <h4 className='text-sm font-medium'>
              {t('TTFT-aware routing defaults')}
            </h4>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t(
                'Global thresholds for TTFT-aware weighted proxy selection. They apply to channels that enable it in channel settings.'
              )}
            </p>
          </div>
          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3'>
            {TTFT_ROUTING_FIELDS.map((f) => (
              <FormField
                key={f.name}
                control={ttftForm.control}
                name={f.name}
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t(f.label)}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        step={f.step}
                        {...field}
                        value={field.value ?? ''}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            ))}
          </div>
          <div>
            <Button type='submit' disabled={updateOption.isPending}>
              {t('Save TTFT routing')}
            </Button>
          </div>
        </SettingsForm>
      </Form>

      <AlertDialog
        open={lockIndex !== null}
        onOpenChange={(open) => {
          if (!open) setLockIndex(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Lock this proxy?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Once locked, every field except the description can no longer be edited, and the lock cannot be removed. To change a locked proxy, add a new entry. Remember to save after locking.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction variant='destructive' onClick={confirmLock}>
              {t('Lock')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
