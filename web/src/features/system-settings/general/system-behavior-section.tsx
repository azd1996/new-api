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

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
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
import type {
  ClientDisconnectRule,
  DebugScopeRule,
  GroupModelRule,
} from '../types'
import { ClaudeVolatileTailPatternsField } from './claude-volatile-tail-patterns-field'
import { ClientDisconnectRulesField } from './client-disconnect-rules-field'
import { DebugScopeRulesField } from './debug-scope-rules-field'
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
    'cache_breakpoint_fix_setting.rules': GroupModelRule[]
    'stable_base_breakpoint_setting.rules': GroupModelRule[]
    'fallback_cache_breakpoint_setting.rules': GroupModelRule[]
    'retain_previous_breakpoint_setting.rules': GroupModelRule[]
    'prompt_cache_key_injection_setting.rules': GroupModelRule[]
    'claude_volatile_tail_pattern_setting.patterns': string[]
    'client_disconnect_setting.rules': ClientDisconnectRule[]
    'debug_scope_setting.rules': DebugScopeRule[]
  }
}

export function SystemBehaviorSection(props: SystemBehaviorSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const booleanDefaults: BehaviorFormValues = {
    DefaultCollapseSidebar: props.defaultValues.DefaultCollapseSidebar,
    DemoSiteEnabled: props.defaultValues.DemoSiteEnabled,
    SelfUseModeEnabled: props.defaultValues.SelfUseModeEnabled,
    AllowUnsetModelRatio: props.defaultValues.AllowUnsetModelRatio,
    DebugEnabled: props.defaultValues.DebugEnabled,
  }

  const form = useForm({
    resolver: zodResolver(behaviorSchema),
    defaultValues: booleanDefaults,
  })

  useResetForm(form, booleanDefaults)

  const [extendedCacheRules, setExtendedCacheRules] = useState(
    props.defaultValues['extended_cache_ttl_setting.rules']
  )
  const [metadataDisableRules, setMetadataDisableRules] = useState(
    props.defaultValues['metadata_injection_disable_setting.rules']
  )
  const [breakpointFixRules, setBreakpointFixRules] = useState(
    props.defaultValues['cache_breakpoint_fix_setting.rules']
  )
  const [stableBaseRules, setStableBaseRules] = useState(
    props.defaultValues['stable_base_breakpoint_setting.rules']
  )
  const [fallbackBreakpointRules, setFallbackBreakpointRules] = useState(
    props.defaultValues['fallback_cache_breakpoint_setting.rules']
  )
  const [retainPreviousRules, setRetainPreviousRules] = useState(
    props.defaultValues['retain_previous_breakpoint_setting.rules']
  )
  const [promptCacheKeyRules, setPromptCacheKeyRules] = useState(
    props.defaultValues['prompt_cache_key_injection_setting.rules']
  )
  const [volatileTailPatterns, setVolatileTailPatterns] = useState(
    props.defaultValues['claude_volatile_tail_pattern_setting.patterns']
  )
  const [clientDisconnectRules, setClientDisconnectRules] = useState(
    props.defaultValues['client_disconnect_setting.rules']
  )
  const [debugScopeRules, setDebugScopeRules] = useState(
    props.defaultValues['debug_scope_setting.rules']
  )

  useEffect(() => {
    setExtendedCacheRules(
      props.defaultValues['extended_cache_ttl_setting.rules']
    )
    setMetadataDisableRules(
      props.defaultValues['metadata_injection_disable_setting.rules']
    )
    setBreakpointFixRules(
      props.defaultValues['cache_breakpoint_fix_setting.rules']
    )
    setStableBaseRules(
      props.defaultValues['stable_base_breakpoint_setting.rules']
    )
    setFallbackBreakpointRules(
      props.defaultValues['fallback_cache_breakpoint_setting.rules']
    )
    setRetainPreviousRules(
      props.defaultValues['retain_previous_breakpoint_setting.rules']
    )
    setPromptCacheKeyRules(
      props.defaultValues['prompt_cache_key_injection_setting.rules']
    )
    setVolatileTailPatterns(
      props.defaultValues['claude_volatile_tail_pattern_setting.patterns']
    )
    setClientDisconnectRules(
      props.defaultValues['client_disconnect_setting.rules']
    )
    setDebugScopeRules(props.defaultValues['debug_scope_setting.rules'])
  }, [props.defaultValues])

  const [confirmDisableRatioOpen, setConfirmDisableRatioOpen] = useState(false)

  const confirmDisableAllowUnsetModelRatio = () => {
    form.setValue('AllowUnsetModelRatio', false, { shouldDirty: true })
    setConfirmDisableRatioOpen(false)
  }

  const onSubmit = async (data: BehaviorFormValues) => {
    const updates: { key: string; value: string | boolean }[] = (
      Object.entries(data) as [keyof BehaviorFormValues, boolean][]
    )
      .filter(([key, value]) => value !== booleanDefaults[key])
      .map(([key, value]) => ({ key, value }))

    const extendedSerialized = JSON.stringify(extendedCacheRules)
    if (
      extendedSerialized !==
      JSON.stringify(props.defaultValues['extended_cache_ttl_setting.rules'])
    ) {
      updates.push({
        key: 'extended_cache_ttl_setting.rules',
        value: extendedSerialized,
      })
    }

    const metadataSerialized = JSON.stringify(metadataDisableRules)
    if (
      metadataSerialized !==
      JSON.stringify(
        props.defaultValues['metadata_injection_disable_setting.rules']
      )
    ) {
      updates.push({
        key: 'metadata_injection_disable_setting.rules',
        value: metadataSerialized,
      })
    }

    const breakpointFixSerialized = JSON.stringify(breakpointFixRules)
    if (
      breakpointFixSerialized !==
      JSON.stringify(props.defaultValues['cache_breakpoint_fix_setting.rules'])
    ) {
      updates.push({
        key: 'cache_breakpoint_fix_setting.rules',
        value: breakpointFixSerialized,
      })
    }

    const stableBaseSerialized = JSON.stringify(stableBaseRules)
    if (
      stableBaseSerialized !==
      JSON.stringify(props.defaultValues['stable_base_breakpoint_setting.rules'])
    ) {
      updates.push({
        key: 'stable_base_breakpoint_setting.rules',
        value: stableBaseSerialized,
      })
    }

    const fallbackBreakpointSerialized = JSON.stringify(
      fallbackBreakpointRules
    )
    if (
      fallbackBreakpointSerialized !==
      JSON.stringify(
        props.defaultValues['fallback_cache_breakpoint_setting.rules']
      )
    ) {
      updates.push({
        key: 'fallback_cache_breakpoint_setting.rules',
        value: fallbackBreakpointSerialized,
      })
    }

    const retainPreviousSerialized = JSON.stringify(retainPreviousRules)
    if (
      retainPreviousSerialized !==
      JSON.stringify(
        props.defaultValues['retain_previous_breakpoint_setting.rules']
      )
    ) {
      updates.push({
        key: 'retain_previous_breakpoint_setting.rules',
        value: retainPreviousSerialized,
      })
    }

    const promptCacheKeySerialized = JSON.stringify(promptCacheKeyRules)
    if (
      promptCacheKeySerialized !==
      JSON.stringify(
        props.defaultValues['prompt_cache_key_injection_setting.rules']
      )
    ) {
      updates.push({
        key: 'prompt_cache_key_injection_setting.rules',
        value: promptCacheKeySerialized,
      })
    }

    const volatileTailPatternsSerialized = JSON.stringify(
      volatileTailPatterns
    )
    if (
      volatileTailPatternsSerialized !==
      JSON.stringify(
        props.defaultValues['claude_volatile_tail_pattern_setting.patterns']
      )
    ) {
      updates.push({
        key: 'claude_volatile_tail_pattern_setting.patterns',
        value: volatileTailPatternsSerialized,
      })
    }

    const clientDisconnectSerialized = JSON.stringify(clientDisconnectRules)
    if (
      clientDisconnectSerialized !==
      JSON.stringify(props.defaultValues['client_disconnect_setting.rules'])
    ) {
      updates.push({
        key: 'client_disconnect_setting.rules',
        value: clientDisconnectSerialized,
      })
    }

    const debugScopeSerialized = JSON.stringify(debugScopeRules)
    if (
      debugScopeSerialized !==
      JSON.stringify(props.defaultValues['debug_scope_setting.rules'])
    ) {
      updates.push({
        key: 'debug_scope_setting.rules',
        value: debugScopeSerialized,
      })
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  return (
    <>
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
                      onCheckedChange={(checked) => {
                        if (checked) return
                        field.onChange(false)
                      }}
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
                      onCheckedChange={(checked) => {
                        if (checked) return
                        field.onChange(false)
                      }}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <FormField
              control={form.control}
              name='AllowUnsetModelRatio'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>
                      {t('Allow Models Without Configured Price')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'When enabled, requests to models without a configured price are allowed and billed at the fallback ratio instead of being rejected'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={(checked) => {
                        if (!checked) {
                          setConfirmDisableRatioOpen(true)
                          return
                        }
                        field.onChange(true)
                      }}
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

            <ClientDisconnectRulesField
              label={t('Abort Upstream on Client Disconnect (API Key + Model)')}
              description={t(
                'Abort the upstream stream when the client disconnects, only for the listed API key (token name) + model pairs. Both fields are required per rule; add multiple rows to cover more scopes. Leave empty to keep the upstream alive and bill the final usage frame.'
              )}
              value={clientDisconnectRules}
              onChange={setClientDisconnectRules}
            />

            <Separator />

            <DebugScopeRulesField
              label={t('Scoped Debug Logging (API Key + Model)')}
              description={t(
                'Log full request/response bodies only for the listed API key (token name) + model pairs, even when global Debug Mode is off. Both fields are required per rule; add multiple rows to cover more scopes. Leave empty to disable scoped debug logging.'
              )}
              value={debugScopeRules}
              onChange={setDebugScopeRules}
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

            <GroupModelRulesField
              label={t('Fix Misplaced Cache Breakpoints')}
              description={t(
                'For the listed group and model combinations (Claude), move a prompt-cache breakpoint placed on a volatile trailing reminder (such as <total_tokens>) onto the last stable block before it. Leave empty to disable. Independent of Stable Base Breakpoint below.'
              )}
              value={breakpointFixRules}
              onChange={setBreakpointFixRules}
            />

            <GroupModelRulesField
              label={t('Stable Base Breakpoint')}
              description={t(
                'For the listed group and model combinations (Claude), add an independent cache breakpoint protecting the tools/system prefix, regardless of growing message history. Leave empty to disable. Independent of Fix Misplaced Cache Breakpoints above.'
              )}
              value={stableBaseRules}
              onChange={setStableBaseRules}
            />

            <GroupModelRulesField
              label={t('Fallback Cache Breakpoint')}
              description={t(
                'For the listed group and model combinations (Claude), add a cache breakpoint when the request carries no cache marker at all. Leave empty to disable.'
              )}
              value={fallbackBreakpointRules}
              onChange={setFallbackBreakpointRules}
            />

            <GroupModelRulesField
              label={t('Retain Previous Cache Breakpoint')}
              description={t(
                'For the listed group and model combinations (Claude), restore the previous turn’s cache breakpoint when the matching prefix is unchanged. Leave empty to disable. Independent of whether cache diagnostics logging is on.'
              )}
              value={retainPreviousRules}
              onChange={setRetainPreviousRules}
            />

            <ClaudeVolatileTailPatternsField
              value={volatileTailPatterns}
              onChange={setVolatileTailPatterns}
            />

            <GroupModelRulesField
              label={t('Inject prompt_cache_key')}
              description={t(
                'For the listed group and model combinations (OpenAI), give requests without a prompt_cache_key one derived from the conversation, so its turns reach the cached prefix. Leave empty to disable.'
              )}
              value={promptCacheKeyRules}
              onChange={setPromptCacheKeyRules}
            />
          </SettingsForm>
        </Form>
      </SettingsSection>
      <Dialog
        open={confirmDisableRatioOpen}
        onOpenChange={setConfirmDisableRatioOpen}
        title={t('Confirm disabling this option')}
        contentClassName='sm:max-w-md'
        footer={
          <>
            <Button
              variant='outline'
              onClick={() => setConfirmDisableRatioOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              variant='destructive'
              onClick={confirmDisableAllowUnsetModelRatio}
            >
              {t('Confirm Disable')}
            </Button>
          </>
        }
      >
        <div className='text-muted-foreground text-sm'>
          {t(
            'After disabling, requests to models that have no configured price will be rejected instead of billed at the fallback ratio. Any in-use model that is not yet priced will immediately become unavailable, which may break live traffic. Please confirm you understand the risk before turning it off.'
          )}
        </div>
      </Dialog>
    </>
  )
}
