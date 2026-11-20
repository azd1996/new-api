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
import { Plus, Trash2 } from 'lucide-react'
import { useEffect } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
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

import {
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { BalanceAlertRule } from '../types'

const balanceAlertRuleSchema = z.object({
  id: z.string().trim().min(1, 'Rule ID is required'),
  enabled: z.boolean(),
  group: z.string(),
  threshold_amount: z.coerce
    .number<number | string>()
    .positive('Enter a threshold above zero'),
  max_users: z.coerce.number<number | string>().int().min(1).max(100),
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

const balanceAlertSchema = z.object({
  enabled: z.boolean(),
  rules: z.array(balanceAlertRuleSchema),
})

type BalanceAlertFormInput = z.input<typeof balanceAlertSchema>
type BalanceAlertFormValues = z.output<typeof balanceAlertSchema>

type BalanceAlertSettingsSectionProps = {
  defaultValues: {
    'balance_alert_setting.enabled': boolean
    'balance_alert_setting.rules': BalanceAlertRule[]
  }
}

function seedRules(rules: BalanceAlertRule[]): BalanceAlertFormValues['rules'] {
  return rules.map((rule) => ({
    id: rule.id ?? '',
    enabled: rule.enabled ?? false,
    group: rule.group ?? '',
    threshold_amount: rule.threshold_amount || 50,
    max_users: rule.max_users || 20,
    alert_toid: (rule.alert_toid ?? []).join(', '),
    alert_url: rule.alert_url ?? '',
    link: rule.link ?? '',
    at_user_ids: (rule.at_user_ids ?? []).join(', '),
  }))
}

function serializeRules(
  rules: BalanceAlertFormValues['rules']
): BalanceAlertRule[] {
  return rules.map((rule) => ({
    id: rule.id.trim(),
    enabled: rule.enabled,
    group: rule.group.trim(),
    threshold_amount: rule.threshold_amount,
    max_users: rule.max_users,
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

export function BalanceAlertSettingsSection(
  props: BalanceAlertSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<BalanceAlertFormInput, unknown, BalanceAlertFormValues>({
    defaultValues: {
      enabled: props.defaultValues['balance_alert_setting.enabled'],
      rules: seedRules(props.defaultValues['balance_alert_setting.rules']),
    },
    resolver: zodResolver(balanceAlertSchema),
  })
  const fieldArray = useFieldArray({ control: form.control, name: 'rules' })

  useEffect(() => {
    form.reset({
      enabled: props.defaultValues['balance_alert_setting.enabled'],
      rules: seedRules(props.defaultValues['balance_alert_setting.rules']),
    })
  }, [form, props.defaultValues])

  const onSubmit = async (values: BalanceAlertFormValues) => {
    await updateOption.mutateAsync({
      key: 'balance_alert_setting.enabled',
      value: values.enabled,
    })
    await updateOption.mutateAsync({
      key: 'balance_alert_setting.rules',
      value: JSON.stringify(serializeRules(values.rules)),
    })
  }

  return (
    <SettingsSection title={t('Low balance alerts')} className='gap-6'>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save balance alert settings'
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable low balance alerts')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Check enabled users once an hour and send the users below a rule threshold to Ruliu, at most once per user per day.'
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
              {t('No balance alert rules configured.')}
            </div>
          ) : (
            fieldArray.fields.map((field, index) => (
              <div key={field.id} className='space-y-4 rounded-lg border p-4'>
                <div className='flex items-center justify-between gap-3'>
                  <h4 className='font-medium'>
                    {t('Balance alert rule')} {index + 1}
                  </h4>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    aria-label={t('Remove balance alert rule')}
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
                        <FormLabel>{t('Enable this rule')}</FormLabel>
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
                            placeholder='balance-default'
                            {...inputField}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.group`}
                    render={({ field: inputField }) => (
                      <FormItem>
                        <FormLabel>{t('User group')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder={t('Leave empty for every group')}
                            {...inputField}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.threshold_amount`}
                    render={({ field: inputField }) => (
                      <FormItem>
                        <FormLabel>{t('Balance threshold')}</FormLabel>
                        <FormControl>
                          <Input type='number' step='0.01' {...inputField} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.max_users`}
                    render={({ field: inputField }) => (
                      <FormItem>
                        <FormLabel>{t('Max users per message')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={100}
                            {...inputField}
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
                          <Input placeholder='123, 456' {...inputField} />
                        </FormControl>
                        <FormDescription>
                          {t('Comma-separated recipient IDs.')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.alert_url`}
                    render={({ field: inputField }) => (
                      <FormItem>
                        <FormLabel>{t('Alert URL')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder='https://example.com/ruliu/hook'
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
                        <FormLabel>{t('Link')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder='https://example.com/users'
                            {...inputField}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name={`rules.${index}.at_user_ids`}
                    render={({ field: inputField }) => (
                      <FormItem>
                        <FormLabel>{t('Mention user IDs')}</FormLabel>
                        <FormControl>
                          <Input placeholder='zhangsan, lisi' {...inputField} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </SettingsFormGrid>
              </div>
            ))
          )}
          <Button
            type='button'
            variant='outline'
            onClick={() =>
              fieldArray.append({
                id: `rule-${fieldArray.fields.length + 1}`,
                enabled: true,
                group: '',
                threshold_amount: 50,
                max_users: 20,
                alert_toid: '',
                alert_url: '',
                link: '',
                at_user_ids: '',
              })
            }
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add balance alert rule')}
          </Button>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
