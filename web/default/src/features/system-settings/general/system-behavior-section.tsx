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
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import type { GroupModelRule } from '../types'
import { GroupModelRulesField } from './group-model-rules-field'

const behaviorSchema = z.object({
  DefaultCollapseSidebar: z.boolean(),
  DemoSiteEnabled: z.boolean(),
  SelfUseModeEnabled: z.boolean(),
  AllowUnsetModelRatio: z.boolean(),
  DebugEnabled: z.boolean(),
})

type BehaviorFormValues = z.infer<typeof behaviorSchema>

type SystemBehaviorSectionProps = {
  defaultValues: BehaviorFormValues & {
    'extended_cache_ttl_setting.rules': GroupModelRule[]
    'metadata_injection_disable_setting.rules': GroupModelRule[]
  }
}

export function SystemBehaviorSection({
  defaultValues,
}: SystemBehaviorSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const booleanDefaults: BehaviorFormValues = {
    DefaultCollapseSidebar: defaultValues.DefaultCollapseSidebar,
    DemoSiteEnabled: defaultValues.DemoSiteEnabled,
    SelfUseModeEnabled: defaultValues.SelfUseModeEnabled,
    AllowUnsetModelRatio: defaultValues.AllowUnsetModelRatio,
    DebugEnabled: defaultValues.DebugEnabled,
  }

  const form = useForm({
    resolver: zodResolver(behaviorSchema),
    defaultValues: booleanDefaults,
  })

  useResetForm(form, booleanDefaults)

  const [extendedCacheRules, setExtendedCacheRules] = useState(
    defaultValues['extended_cache_ttl_setting.rules']
  )
  const [metadataDisableRules, setMetadataDisableRules] = useState(
    defaultValues['metadata_injection_disable_setting.rules']
  )

  useEffect(() => {
    setExtendedCacheRules(defaultValues['extended_cache_ttl_setting.rules'])
    setMetadataDisableRules(
      defaultValues['metadata_injection_disable_setting.rules']
    )
  }, [defaultValues])

  const onSubmit = async (data: BehaviorFormValues) => {
    const updates: { key: string; value: string | boolean }[] = (
      Object.entries(data) as [keyof BehaviorFormValues, boolean][]
    )
      .filter(([key, value]) => value !== booleanDefaults[key])
      .map(([key, value]) => ({ key, value }))

    const extendedSerialized = JSON.stringify(extendedCacheRules)
    if (
      extendedSerialized !==
      JSON.stringify(defaultValues['extended_cache_ttl_setting.rules'])
    ) {
      updates.push({
        key: 'extended_cache_ttl_setting.rules',
        value: extendedSerialized,
      })
    }

    const metadataSerialized = JSON.stringify(metadataDisableRules)
    if (
      metadataSerialized !==
      JSON.stringify(defaultValues['metadata_injection_disable_setting.rules'])
    ) {
      updates.push({
        key: 'metadata_injection_disable_setting.rules',
        value: metadataSerialized,
      })
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  return (
    <SettingsSection title={t('System Behavior')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='DefaultCollapseSidebar'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Default Collapse Sidebar')}</FormLabel>
                  <FormDescription>
                    {t('Sidebar collapsed by default for new users')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='DemoSiteEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Demo Site Mode')}</FormLabel>
                  <FormDescription>
                    {t('Enable demo mode with limited functionality')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='SelfUseModeEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Self-Use Mode')}</FormLabel>
                  <FormDescription>
                    {t('Optimize system for self-hosted single-user usage')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='DebugEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Debug Mode')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Output verbose debug logs. Takes effect immediately without a restart.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <Separator />

          <GroupModelRulesField
            label={t('Force 1h Prompt Cache TTL')}
            description={t(
              'Force the 1-hour prompt-cache TTL for the listed group and model combinations (Claude). Leave empty to disable.'
            )}
            value={extendedCacheRules}
            onChange={setExtendedCacheRules}
          />

          <GroupModelRulesField
            label={t('Disable Metadata Injection')}
            description={t(
              'Disable Claude metadata (user_id / session_id) injection for the listed group and model combinations. Leave empty to inject for all.'
            )}
            value={metadataDisableRules}
            onChange={setMetadataDisableRules}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
