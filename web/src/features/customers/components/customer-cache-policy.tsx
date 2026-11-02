import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import type { CustomerFormValues } from '../lib/customer-form'
import type { ClaudeCacheOptimization } from '../types'

const prefixOptions = [
  'claude-',
  'claude-sonnet-',
  'claude-opus-',
  'claude-haiku-',
].map((value) => ({ value, label: value }))

export function CustomerCachePolicy() {
  const { t } = useTranslation()
  const form = useFormContext<CustomerFormValues>()
  const policy = useWatch({
    control: form.control,
    name: 'claude_cache_optimization',
  })
  const rules: {
    key: keyof ClaudeCacheOptimization
    title: string
    description: string
  }[] = [
    {
      key: 'force_extended_ttl',
      title: t('Extended cache TTL'),
      description: t(
        'Use a one-hour cache lifetime to reuse prompts across longer gaps.'
      ),
    },
    {
      key: 'fix_volatile_breakpoint',
      title: t('Fix volatile breakpoint'),
      description: t(
        'Place a cache breakpoint before changing trailing system content.'
      ),
    },
    {
      key: 'retain_previous_breakpoint',
      title: t('Retain previous breakpoint'),
      description: t(
        'Keep the previous turn’s cache boundary reachable as the conversation grows.'
      ),
    },
    {
      key: 'stable_base_breakpoint',
      title: t('Stable base breakpoint'),
      description: t(
        'Cache the stable tools and system prefix for reuse across turns.'
      ),
    },
    {
      key: 'fallback_cache_breakpoint',
      title: t('Fallback cache breakpoint'),
      description: t(
        'Add a fallback cache breakpoint when the request has no cache markers.'
      ),
    },
  ]

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader title={t('Claude cache optimization')} />
      {!policy ? (
        <>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Inheriting existing gateway behavior. Saving other customer details keeps this inheritance.'
            )}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Customizing replaces all inherited rules. All start disabled; enable the rules you need before saving.'
            )}
          </p>
          <Button
            type='button'
            variant='outline'
            onClick={() =>
              form.setValue(
                'claude_cache_optimization',
                {
                  force_extended_ttl: { enabled: false, model_prefixes: [] },
                  fix_volatile_breakpoint: {
                    enabled: false,
                    model_prefixes: [],
                  },
                  retain_previous_breakpoint: {
                    enabled: false,
                    model_prefixes: [],
                  },
                  stable_base_breakpoint: {
                    enabled: false,
                    model_prefixes: [],
                  },
                  fallback_cache_breakpoint: {
                    enabled: false,
                    model_prefixes: [],
                  },
                },
                { shouldDirty: true }
              )
            }
          >
            {t('Customize cache rules')}
          </Button>
        </>
      ) : (
        <>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Enabled rules with no prefixes apply to all supported models. Prefixes match the start of model names literally, without wildcards. Disabling a rule keeps its selections.'
            )}
          </p>
          {rules.map((rule) => (
            <div key={rule.key} className='space-y-3 rounded-md border p-3'>
              <div className='flex items-start justify-between gap-3'>
                <div className='space-y-1'>
                  <Label htmlFor={`cache-${rule.key}`}>{rule.title}</Label>
                  <p
                    id={`cache-${rule.key}-description`}
                    className='text-muted-foreground text-xs'
                  >
                    {rule.description}
                  </p>
                </div>
                <Switch
                  id={`cache-${rule.key}`}
                  aria-describedby={`cache-${rule.key}-description`}
                  checked={policy[rule.key].enabled}
                  onCheckedChange={(enabled) =>
                    form.setValue(
                      `claude_cache_optimization.${rule.key}.enabled`,
                      enabled,
                      { shouldDirty: true }
                    )
                  }
                />
              </div>
              <Label htmlFor={`prefix-${rule.key}`}>
                {t('Model prefixes')}
              </Label>
              <MultiSelect
                id={`prefix-${rule.key}`}
                options={prefixOptions}
                selected={policy[rule.key].model_prefixes}
                allowCreate
                disabled={!policy[rule.key].enabled}
                placeholder={t('All supported models')}
                onChange={(values) =>
                  form.setValue(
                    `claude_cache_optimization.${rule.key}.model_prefixes`,
                    values,
                    { shouldDirty: true }
                  )
                }
              />
            </div>
          ))}
        </>
      )}
    </SideDrawerSection>
  )
}
