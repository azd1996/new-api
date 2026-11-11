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
import { expect, test } from 'vitest'

import { channelSchema } from '../../types'
import {
  channelFormSchema,
  isRequestKeyRenameRules,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'

const storedSetting = {
  upstream_request_id_header: 'X-Upstream-Id',
  response_filter: ['content_filter_results', 'choices.#.logprobs'],
  request_key_rename: [
    { path: 'contents.*.parts.*', keys: { file_data: 'fileData' } },
  ],
  request_key_rename_template_refs: ['gemini-snake-case'],
  request_key_rename_models: ['gemini-2.5-pro'],
  stat_group_ratio: 0.8,
  param_override_template_refs: ['claude-defaults'],
  retry_override_template_refs: ['fallback-429'],
}

function buildChannel(setting: Record<string, unknown>) {
  return channelSchema.parse({
    id: 7,
    type: 1,
    name: 'OpenAI',
    key: '',
    base_url: '',
    models: 'gpt-4o',
    group: 'default',
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    other: '',
    remark: '',
    setting: JSON.stringify(setting),
  })
}

test('the legacy TTFT switch round-trips as TTFT routing and clears when priority is selected', () => {
  const legacyForm = transformChannelToFormDefaults(
    buildChannel({ proxy_ttft_routing_enabled: true })
  )
  expect(legacyForm.proxy_routing_mode).toBe('ttft')

  const prioritySetting = JSON.parse(
    transformFormDataToUpdatePayload(
      {
        ...legacyForm,
        proxy_routing_mode: 'priority',
        proxy_ttft_routing_enabled: false,
      },
      7
    ).setting ?? '{}'
  )
  expect(prioritySetting).not.toHaveProperty('proxy_routing_mode')
  expect(prioritySetting).not.toHaveProperty('proxy_ttft_routing_enabled')
  expect(
    transformChannelToFormDefaults(buildChannel(prioritySetting))
      .proxy_routing_mode
  ).toBe('priority')
})

test('equal routing mode persists without retaining the legacy TTFT switch', () => {
  const form = transformChannelToFormDefaults(
    buildChannel({ proxy_ttft_routing_enabled: true })
  )
  const setting = JSON.parse(
    transformFormDataToUpdatePayload(
      { ...form, proxy_routing_mode: 'equal' },
      7
    ).setting ?? '{}'
  )

  expect(setting.proxy_routing_mode).toBe('equal')
  expect(setting).not.toHaveProperty('proxy_ttft_routing_enabled')
  expect(
    transformChannelToFormDefaults(buildChannel(setting)).proxy_routing_mode
  ).toBe('equal')
})

test('editing a channel keeps the custom setting fields it was loaded with', () => {
  const form = transformChannelToFormDefaults(buildChannel(storedSetting))
  const payload = transformFormDataToUpdatePayload(form, 7)
  expect(JSON.parse(payload.setting ?? '{}')).toMatchObject(storedSetting)
})

test.each([
  { name: 'omitted', rule: { keys: { file_data: 'fileData' } } },
  { name: 'empty', rule: { path: '', keys: { file_data: 'fileData' } } },
  { name: 'dollar', rule: { path: '$', keys: { file_data: 'fileData' } } },
])(
  'an API channel with an $name root rename path validates and preserves its rules on save',
  ({ rule }) => {
    const setting = { ...storedSetting, request_key_rename: [rule] }
    const defaults = transformChannelToFormDefaults(buildChannel(setting))
    const form = channelFormSchema.parse(defaults)
    const payload = transformFormDataToUpdatePayload(form, 7)

    expect(JSON.parse(payload.setting ?? '{}')).toMatchObject(setting)
    expect(JSON.parse(payload.setting ?? '{}').request_key_rename).toEqual([
      rule,
    ])
    expect(isRequestKeyRenameRules([rule])).toBe(true)
  }
)

test.each([null, 0, false, [], {}])(
  'an explicit non-string rename path %j is rejected by channel and template validation',
  (path) => {
    const rules = [{ path, keys: { file_data: 'fileData' } }]
    const defaults = transformChannelToFormDefaults(
      buildChannel({ request_key_rename: rules })
    )
    const result = channelFormSchema.safeParse(defaults)

    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues.map((issue) => issue.path)).toContainEqual([
        'request_key_rename',
      ])
    }
    expect(isRequestKeyRenameRules(rules)).toBe(false)
  }
)

test('a blank or non-positive stat ratio is omitted from the saved setting', () => {
  const form = transformChannelToFormDefaults(buildChannel({}))
  for (const ratio of ['', '0', '-1']) {
    const payload = transformFormDataToUpdatePayload(
      { ...form, stat_group_ratio: ratio, response_filter: '\n  \n' },
      7
    )
    const setting = JSON.parse(payload.setting ?? '{}')
    expect(setting).not.toHaveProperty('stat_group_ratio')
    expect(setting).not.toHaveProperty('response_filter')
  }
})
