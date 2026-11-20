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
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

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

const aiAssistantSchema = z.object({
  enabled: z.boolean(),
  group: z
    .string()
    .trim()
    .min(1, 'Channel group is required')
    .refine((value) => !/\s/.test(value), {
      message: 'Channel group cannot contain whitespace',
    }),
  default_model: z.string().trim().min(1, 'Default model is required'),
  timeout_seconds: z.coerce.number<number | string>().int().min(5).max(300),
  max_tokens: z.coerce.number<number | string>().int().min(1).max(32768),
  temperature: z.coerce.number<number | string>().min(0).max(2),
})

type AIAssistantFormInput = z.input<typeof aiAssistantSchema>
type AIAssistantFormValues = z.output<typeof aiAssistantSchema>

type AIAssistantSettingsSectionProps = {
  defaultValues: {
    'ai_assistant_setting.enabled': boolean
    'ai_assistant_setting.group': string
    'ai_assistant_setting.default_model': string
    'ai_assistant_setting.timeout_seconds': number
    'ai_assistant_setting.max_tokens': number
    'ai_assistant_setting.temperature': number
  }
}

export function AIAssistantSettingsSection(
  props: AIAssistantSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<AIAssistantFormInput, unknown, AIAssistantFormValues>({
    defaultValues: {
      enabled: props.defaultValues['ai_assistant_setting.enabled'],
      group: props.defaultValues['ai_assistant_setting.group'],
      default_model: props.defaultValues['ai_assistant_setting.default_model'],
      timeout_seconds:
        props.defaultValues['ai_assistant_setting.timeout_seconds'],
      max_tokens: props.defaultValues['ai_assistant_setting.max_tokens'],
      temperature: props.defaultValues['ai_assistant_setting.temperature'],
    },
    resolver: zodResolver(aiAssistantSchema),
  })

  useEffect(() => {
    form.reset({
      enabled: props.defaultValues['ai_assistant_setting.enabled'],
      group: props.defaultValues['ai_assistant_setting.group'],
      default_model: props.defaultValues['ai_assistant_setting.default_model'],
      timeout_seconds:
        props.defaultValues['ai_assistant_setting.timeout_seconds'],
      max_tokens: props.defaultValues['ai_assistant_setting.max_tokens'],
      temperature: props.defaultValues['ai_assistant_setting.temperature'],
    })
  }, [form, props.defaultValues])

  const onSubmit = async (values: AIAssistantFormValues) => {
    await updateOption.mutateAsync({
      key: 'ai_assistant_setting.enabled',
      value: values.enabled,
    })
    await updateOption.mutateAsync({
      key: 'ai_assistant_setting.group',
      value: values.group.trim(),
    })
    await updateOption.mutateAsync({
      key: 'ai_assistant_setting.default_model',
      value: values.default_model.trim(),
    })
    await updateOption.mutateAsync({
      key: 'ai_assistant_setting.timeout_seconds',
      value: values.timeout_seconds,
    })
    await updateOption.mutateAsync({
      key: 'ai_assistant_setting.max_tokens',
      value: values.max_tokens,
    })
    await updateOption.mutateAsync({
      key: 'ai_assistant_setting.temperature',
      value: values.temperature,
    })
  }

  return (
    <SettingsSection title={t('AI Assistant')} className='gap-6'>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel={t('Save AI assistant settings')}
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable the AI assistant')}</FormLabel>
                  <FormDescription>
                    {t(
                      'The assistant page and internal LLM calls route through the channel group below.'
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

          <SettingsFormGrid>
            <FormField
              control={form.control}
              name='group'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Channel group')}</FormLabel>
                  <FormControl>
                    <Input placeholder='ai-assistant' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Assistant channels must be assigned to this group. The group does not need to be available to users.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='default_model'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Default model')}</FormLabel>
                  <FormControl>
                    <Input placeholder='gpt-4o' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Used when an internal caller does not specify a model.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='timeout_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Timeout (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={5}
                      max={300}
                      step={1}
                      {...field}
                      value={field.value ?? ''}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Maximum wait time for an assistant response.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='max_tokens'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Maximum tokens')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={32768}
                      step={1}
                      {...field}
                      value={field.value ?? ''}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Completion token limit per assistant request.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='temperature'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Temperature')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={2}
                      step={0.1}
                      {...field}
                      value={field.value ?? ''}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Sampling temperature of the assistant model.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsFormGrid>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
