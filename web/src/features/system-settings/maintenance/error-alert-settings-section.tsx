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
import { useEffect, useMemo } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { getCustomers } from '@/features/customers/api'

import {
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { ErrorAlertRule } from '../types'

const errorAlertRuleSchema = z.object({
  id: z.string().trim().min(1, 'Rule ID is required'),
  enabled: z.boolean(),
  customer_id: z.string().trim().min(1, 'Customer ID is required'),
  top_n: z.coerce.number<number | string>().int().min(1).max(100),
  alert_toid: z.string().trim().min(1, 'At least one group ID is required'),
  alert_url: z
    .string()
    .trim()
    .url('Enter a valid alert URL')
    .refine(
      (value) => /^https?:\/\//i.test(value),
      'Alert URL must use HTTP(S)'
    ),
  link: z.string(),
  at_user_ids: z.string(),
})

const errorAlertSchema = z.object({
  enabled: z.boolean(),
  rules: z.array(errorAlertRuleSchema),
})

type ErrorAlertFormInput = z.input<typeof errorAlertSchema>
type ErrorAlertFormValues = z.output<typeof errorAlertSchema>

type ErrorAlertSettingsSectionProps = {
  defaultValues: {
    'error_alert_setting.enabled': boolean
    'error_alert_setting.rules': ErrorAlertRule[]
  }
}

function seedRules(rules: ErrorAlertRule[]): ErrorAlertFormValues['rules'] {
  return rules.map((rule) => ({
    id: rule.id ?? '',
    enabled: rule.enabled ?? false,
    customer_id: rule.customer_id ?? '',
    top_n: rule.top_n || 15,
    alert_toid: (rule.alert_toid ?? []).join(', '),
    alert_url: rule.alert_url ?? '',
    link: rule.link ?? '',
    at_user_ids: (rule.at_user_ids ?? []).join(', '),
  }))
}

function serializeRules(
  rules: ErrorAlertFormValues['rules']
): ErrorAlertRule[] {
  return rules.map((rule) => ({
    id: rule.id.trim(),
    enabled: rule.enabled,
    customer_id: rule.customer_id.trim(),
    top_n: rule.top_n,
    alert_toid: rule.alert_toid
      .split(',')
      .map((value) => Number(value.trim()))
      .filter((value) => Number.isInteger(value) && value > 0),
    alert_url: rule.alert_url.trim(),
    ...(rule.link.trim() ? { link: rule.link.trim() } : {}),
    ...(rule.at_user_ids.trim()
      ? {
          at_user_ids: rule.at_user_ids
            .split(',')
            .map((value) => value.trim())
            .filter(Boolean),
        }
      : {}),
  }))
}

export function ErrorAlertSettingsSection(
  props: ErrorAlertSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const customersQuery = useQuery({
    queryKey: ['error-alert-customers'],
    queryFn: () => getCustomers({ p: 1, page_size: 1000 }),
    staleTime: 5 * 60 * 1000,
  })
  const customerOptions = useMemo<ComboboxInputOption[]>(
    () =>
      (customersQuery.data?.data?.items ?? []).map((customer) => ({
        value: customer.customer_id,
        label: customer.name
          ? `${customer.customer_id} (${customer.name})`
          : customer.customer_id,
      })),
    [customersQuery.data]
  )
  const form = useForm<ErrorAlertFormInput, unknown, ErrorAlertFormValues>({
    defaultValues: {
      enabled: props.defaultValues['error_alert_setting.enabled'],
      rules: seedRules(props.defaultValues['error_alert_setting.rules']),
    },
    resolver: zodResolver(errorAlertSchema),
  })
  const fieldArray = useFieldArray({ control: form.control, name: 'rules' })

  useEffect(() => {
    form.reset({
      enabled: props.defaultValues['error_alert_setting.enabled'],
      rules: seedRules(props.defaultValues['error_alert_setting.rules']),
    })
  }, [form, props.defaultValues])

  const onSubmit = async (values: ErrorAlertFormValues) => {
    await updateOption.mutateAsync({
      key: 'error_alert_setting.enabled',
      value: values.enabled,
    })
    await updateOption.mutateAsync({
      key: 'error_alert_setting.rules',
      value: JSON.stringify(serializeRules(values.rules)),
    })
  }

  return (
    <SettingsSection title={t('Detailed error alerts')} className='gap-6'>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save error alert settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable detailed error alerts')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Run every 15 minutes and send the previous window error summary for enabled customer rules.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </SettingsSwitchItem>
            )}
          />

          {fieldArray.fields.length === 0 ? (
            <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
              {t('No customer rules configured.')}
            </div>
          ) : (
            <div className='space-y-3'>
              {fieldArray.fields.map((field, index) => (
                <div key={field.id} className='space-y-4 rounded-lg border p-4'>
                  <div className='flex items-center justify-between gap-3'>
                    <h4 className='font-medium'>
                      {t('Customer alert rule')} {index + 1}
                    </h4>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      aria-label={t('Remove customer alert rule')}
                      onClick={() => fieldArray.remove(index)}
                    >
                      <Trash2 className='h-4 w-4' />
                    </Button>
                  </div>
                  <FormField
                    control={form.control}
                    name={`rules.${index}.enabled`}
                    render={({ field: enabledField }) => (
                      <SettingsSwitchItem>
                        <SettingsSwitchContent>
                          <FormLabel>
                            {t('Enable this customer rule')}
                          </FormLabel>
                        </SettingsSwitchContent>
                        <FormControl>
                          <Switch
                            checked={enabledField.value}
                            onCheckedChange={enabledField.onChange}
                          />
                        </FormControl>
                      </SettingsSwitchItem>
                    )}
                  />
                  <SettingsFormGrid>
                    <FormField
                      control={form.control}
                      name={`rules.${index}.id`}
                      render={({ field: inputField }) => (
                        <FormItem>
                          <FormLabel>{t('Rule ID')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder='customer-prod'
                              {...inputField}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name={`rules.${index}.customer_id`}
                      render={({ field: customerField }) => (
                        <FormItem>
                          <FormLabel>{t('Customer ID')}</FormLabel>
                          <FormControl>
                            <Combobox
                              options={customerOptions}
                              value={customerField.value}
                              onValueChange={(value) =>
                                customerField.onChange(value ?? '')
                              }
                              placeholder={t('Select or enter a customer ID')}
                              searchPlaceholder={t('Search customer ID')}
                              emptyText={t('No customer found.')}
                              allowCustomValue
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name={`rules.${index}.top_n`}
                      render={({ field: topNField }) => (
                        <FormItem>
                          <FormLabel>{t('Top error count')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              min={1}
                              max={100}
                              step={1}
                              {...topNField}
                              value={topNField.value ?? ''}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name={`rules.${index}.alert_toid`}
                      render={({ field: inputField }) => (
                        <FormItem>
                          <FormLabel>{t('Ruliu group IDs')}</FormLabel>
                          <FormControl>
                            <Input placeholder='1001, 1002' {...inputField} />
                          </FormControl>
                          <FormDescription>
                            {t('Separate multiple group IDs with commas.')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name={`rules.${index}.alert_url`}
                      render={({ field: inputField }) => (
                        <FormItem className='lg:col-span-2'>
                          <FormLabel>{t('Ruliu alert URL')}</FormLabel>
                          <FormControl>
                            <Input
                              type='url'
                              placeholder='https://example.com/robot/send'
                              {...inputField}
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'The URL is displayed and stored without masking.'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name={`rules.${index}.at_user_ids`}
                      render={({ field: inputField }) => (
                        <FormItem>
                          <FormLabel>{t('Ruliu @ user IDs')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder='user001, user002'
                              {...inputField}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name={`rules.${index}.link`}
                      render={({ field: inputField }) => (
                        <FormItem>
                          <FormLabel>{t('Alert link')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder='https://example.com/logs'
                              {...inputField}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </SettingsFormGrid>
                </div>
              ))}
            </div>
          )}

          <div>
            <Button
              type='button'
              variant='outline'
              onClick={() =>
                fieldArray.append({
                  id: '',
                  enabled: true,
                  customer_id: '',
                  top_n: 15,
                  alert_toid: '',
                  alert_url: '',
                  link: '',
                  at_user_ids: '',
                })
              }
            >
              <Plus className='mr-2 h-4 w-4' />
              {t('Add customer rule')}
            </Button>
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
