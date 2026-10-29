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
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'

import { getProxyHealth } from '../api'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { ProxyHealthEntry, ProxyPoolEntry, ProxyType } from '../types'

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

type ProxyPoolSectionProps = {
  defaultValues: {
    proxies: ProxyPoolEntry[]
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
