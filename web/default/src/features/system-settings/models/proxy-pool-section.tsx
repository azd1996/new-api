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
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { getProxyHealth } from '../api'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { ProxyHealthEntry, ProxyPoolEntry } from '../types'

const PROXY_SCHEME_REGEX = /^(https?|socks5h?):\/\//i

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
  proxies.map((proxy) => ({
    alias: proxy.alias.trim(),
    url: proxy.url.trim(),
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
    defaultValues: { proxies: props.defaultValues.proxies },
  })

  const proxiesWatch = form.watch('proxies')
  const fieldArray = useFieldArray({ control: form.control, name: 'proxies' })

  useEffect(() => {
    baselineRef.current = normalizeProxies(props.defaultValues.proxies)
    form.reset({ proxies: props.defaultValues.proxies })
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
              {fieldArray.fields.map((field, index) => (
                <div
                  key={field.id}
                  className='flex flex-col gap-2 sm:flex-row sm:items-start'
                >
                  <FormField
                    control={form.control}
                    name={`proxies.${index}.alias`}
                    render={({ field: aliasField }) => (
                      <FormItem className='sm:w-48'>
                        <FormControl>
                          <Input
                            placeholder={t('Alias (e.g. hk-1)')}
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
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    aria-label={t('Remove proxy')}
                    onClick={() => fieldArray.remove(index)}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>
              ))}
            </div>
          )}

          <div>
            <Button
              type='button'
              variant='outline'
              onClick={() => fieldArray.append({ alias: '', url: '' })}
            >
              <Plus className='mr-2 h-4 w-4' />
              {t('Add proxy')}
            </Button>
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
