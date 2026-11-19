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
import { Plus, Trash2 } from 'lucide-react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'

import type { CustomerFormValues } from '../lib/customer-form'
import {
  SESSION_IDENTITY_ON_EMPTY_POLICIES,
  SESSION_IDENTITY_SOURCE_TYPES,
  SESSION_IDENTITY_TRANSFORMS,
  createDefaultSessionIdentity,
  createDefaultSessionIdentitySource,
} from '../lib/session-identity'
import type { SessionIdentitySource } from '../types'
import { CustomerSessionIdentityTest } from './customer-session-identity-test'

const prefixOptions = [
  'claude-',
  'claude-sonnet-',
  'claude-opus-',
  'claude-haiku-',
].map((value) => ({ value, label: value }))

type CustomerSessionIdentityProps = {
  customerId?: string
}

export function CustomerSessionIdentity(props: CustomerSessionIdentityProps) {
  const { t } = useTranslation()
  const form = useFormContext<CustomerFormValues>()
  const policy = useWatch({
    control: form.control,
    name: 'session_identity',
  })

  const setPolicy = (value: CustomerFormValues['session_identity']) => {
    form.setValue('session_identity', value, { shouldDirty: true })
  }

  const updateSource = (
    index: number,
    patch: Partial<SessionIdentitySource>
  ) => {
    if (!policy) return
    const sources = policy.sources.map((source, sourceIndex) =>
      sourceIndex === index ? { ...source, ...patch } : source
    )
    setPolicy({ ...policy, sources })
  }

  return (
    <>
      <SideDrawerSection>
        <SideDrawerSectionHeader
          title={t('Session identity extraction')}
          description={t(
            'Custom extraction supplies a stable conversation identity for channel affinity and upstream session stickiness.'
          )}
        />
        {!policy ? (
          <>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Inheriting existing gateway behavior. Saving other customer details keeps this inheritance.'
              )}
            </p>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Customizing replaces the inherited behavior with your own extraction sources. Client-provided identities always take precedence.'
              )}
            </p>
            <Button
              type='button'
              variant='outline'
              onClick={() => setPolicy(createDefaultSessionIdentity())}
            >
              {t('Customize session identity')}
            </Button>
          </>
        ) : (
          <div className='space-y-3 rounded-md border p-3'>
            <div className='flex items-start justify-between gap-3'>
              <div className='space-y-1'>
                <Label htmlFor='session-identity-enabled'>
                  {t('Enable extraction')}
                </Label>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'When disabled, the gateway falls back to its default heuristics.'
                  )}
                </p>
              </div>
              <Switch
                id='session-identity-enabled'
                checked={policy.enabled}
                onCheckedChange={(enabled) => setPolicy({ ...policy, enabled })}
              />
            </div>

            <fieldset
              disabled={!policy.enabled}
              className='space-y-4 disabled:opacity-60'
            >
              <div className='space-y-2'>
                <Label htmlFor='session-identity-prefixes'>
                  {t('Model prefixes')}
                </Label>
                <MultiSelect
                  id='session-identity-prefixes'
                  options={prefixOptions}
                  selected={policy.model_prefixes}
                  allowCreate
                  placeholder={t('All supported models')}
                  onChange={(values) =>
                    setPolicy({ ...policy, model_prefixes: values })
                  }
                />
              </div>

              <div className='space-y-2'>
                <div className='flex items-center justify-between'>
                  <Label>{t('Extraction sources')}</Label>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      setPolicy({
                        ...policy,
                        sources: [
                          ...policy.sources,
                          createDefaultSessionIdentitySource(),
                        ],
                      })
                    }
                  >
                    <Plus className='mr-1 h-3 w-3' />
                    {t('Add source')}
                  </Button>
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Sources are tried in order. The first non-empty value wins.'
                  )}
                </p>
                {policy.sources.length === 0 && (
                  <p className='text-muted-foreground text-sm'>
                    {t('No extraction sources configured.')}
                  </p>
                )}
                {policy.sources.map((source, index) => (
                  <div
                    key={`${source.type}:${source.key}:${source.path}:${source.pattern}:${source.json_string_field}:${source.capture_group}`}
                    className='space-y-2 rounded-md border p-3'
                  >
                    <div className='flex items-center gap-2'>
                      <NativeSelect
                        value={source.type}
                        aria-label={t('Source type')}
                        className='min-w-0 flex-1'
                        onChange={(event) =>
                          updateSource(index, {
                            type: event.target
                              .value as SessionIdentitySource['type'],
                          })
                        }
                      >
                        {SESSION_IDENTITY_SOURCE_TYPES.map((type) => (
                          <NativeSelectOption key={type} value={type}>
                            {t(type)}
                          </NativeSelectOption>
                        ))}
                      </NativeSelect>
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon'
                        aria-label={t('Remove source')}
                        onClick={() =>
                          setPolicy({
                            ...policy,
                            sources: policy.sources.filter(
                              (_, sourceIndex) => sourceIndex !== index
                            ),
                          })
                        }
                      >
                        <Trash2 className='text-destructive h-4 w-4' />
                      </Button>
                    </div>
                    {source.type === 'request_header' && (
                      <div className='space-y-1'>
                        <Label htmlFor={`source-${index}-key`}>
                          {t('Header name')}
                        </Label>
                        <Input
                          id={`source-${index}-key`}
                          value={source.key}
                          placeholder='x-claude-code-session-id'
                          onChange={(event) =>
                            updateSource(index, { key: event.target.value })
                          }
                        />
                      </div>
                    )}
                    {(source.type === 'gjson' || source.type === 'regex') && (
                      <div className='space-y-1'>
                        <Label htmlFor={`source-${index}-path`}>
                          {t('Field path')}
                        </Label>
                        <Input
                          id={`source-${index}-path`}
                          value={source.path}
                          placeholder='system.0.text'
                          onChange={(event) =>
                            updateSource(index, { path: event.target.value })
                          }
                        />
                      </div>
                    )}
                    {(source.type === 'metadata_user_id' ||
                      source.type === 'gjson') && (
                      <div className='space-y-1'>
                        <Label htmlFor={`source-${index}-json-field`}>
                          {t('Inner JSON field (optional)')}
                        </Label>
                        <Input
                          id={`source-${index}-json-field`}
                          value={source.json_string_field}
                          placeholder='session_id'
                          onChange={(event) =>
                            updateSource(index, {
                              json_string_field: event.target.value,
                            })
                          }
                        />
                      </div>
                    )}
                    {source.type === 'regex' && (
                      <div className='grid gap-2 sm:grid-cols-[1fr_7rem]'>
                        <div className='space-y-1'>
                          <Label htmlFor={`source-${index}-pattern`}>
                            {t('Pattern')}
                          </Label>
                          <Input
                            id={`source-${index}-pattern`}
                            value={source.pattern}
                            placeholder='^\\[([^\\]]+)-fixed-\\d+\\]'
                            onChange={(event) =>
                              updateSource(index, {
                                pattern: event.target.value,
                              })
                            }
                          />
                        </div>
                        <div className='space-y-1'>
                          <Label htmlFor={`source-${index}-capture`}>
                            {t('Capture group')}
                          </Label>
                          <Input
                            id={`source-${index}-capture`}
                            type='number'
                            min={0}
                            value={source.capture_group}
                            onChange={(event) =>
                              updateSource(index, {
                                capture_group:
                                  Number.parseInt(event.target.value, 10) || 0,
                              })
                            }
                          />
                        </div>
                      </div>
                    )}
                  </div>
                ))}
              </div>

              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='space-y-1'>
                  <Label htmlFor='session-identity-transform'>
                    {t('Value transform')}
                  </Label>
                  <NativeSelect
                    id='session-identity-transform'
                    className='w-full'
                    value={policy.transform}
                    onChange={(event) =>
                      setPolicy({ ...policy, transform: event.target.value })
                    }
                  >
                    {SESSION_IDENTITY_TRANSFORMS.map((option) => (
                      <NativeSelectOption
                        key={option.value}
                        value={option.value}
                      >
                        {t(option.labelKey)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </div>
                <div className='space-y-1'>
                  <Label htmlFor='session-identity-on-empty'>
                    {t('When no source matches')}
                  </Label>
                  <NativeSelect
                    id='session-identity-on-empty'
                    className='w-full'
                    value={policy.on_empty}
                    onChange={(event) =>
                      setPolicy({ ...policy, on_empty: event.target.value })
                    }
                  >
                    {SESSION_IDENTITY_ON_EMPTY_POLICIES.map((option) => (
                      <NativeSelectOption
                        key={option.value}
                        value={option.value}
                      >
                        {t(option.labelKey)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                </div>
                <div className='space-y-1'>
                  <Label htmlFor='session-identity-value-regex'>
                    {t('Value validation regex')}
                  </Label>
                  <Input
                    id='session-identity-value-regex'
                    value={policy.value_regex}
                    placeholder='^[A-Za-z0-9_-]{4,64}$'
                    onChange={(event) =>
                      setPolicy({
                        ...policy,
                        value_regex: event.target.value,
                      })
                    }
                  />
                </div>
                <div className='space-y-1'>
                  <Label htmlFor='session-identity-ttl'>
                    {t('Affinity TTL (seconds, 0 = default)')}
                  </Label>
                  <Input
                    id='session-identity-ttl'
                    type='number'
                    min={0}
                    value={policy.ttl_seconds}
                    onChange={(event) =>
                      setPolicy({
                        ...policy,
                        ttl_seconds:
                          Number.parseInt(event.target.value, 10) || 0,
                      })
                    }
                  />
                </div>
              </div>

              <div className='flex items-center justify-between gap-3 rounded-md border p-3'>
                <div className='space-y-1'>
                  <Label htmlFor='session-identity-first-message'>
                    {t('Skip first-message fallback')}
                  </Label>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Prevents a rotating identity for clients that send only the latest turn.'
                    )}
                  </p>
                </div>
                <Switch
                  id='session-identity-first-message'
                  checked={policy.disable_first_message_fallback}
                  onCheckedChange={(checked) =>
                    setPolicy({
                      ...policy,
                      disable_first_message_fallback: checked,
                    })
                  }
                />
              </div>
            </fieldset>
          </div>
        )}
      </SideDrawerSection>
      <CustomerSessionIdentityTest
        config={policy}
        customerId={props.customerId}
      />
    </>
  )
}
