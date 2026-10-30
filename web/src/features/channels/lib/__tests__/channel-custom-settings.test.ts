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
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from '../channel-form'

const storedSetting = {
  upstream_request_id_header: 'X-Upstream-Id',
  response_filter: ['content_filter_results', 'choices.#.logprobs'],
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

test('editing a channel keeps the custom setting fields it was loaded with', () => {
  const form = transformChannelToFormDefaults(buildChannel(storedSetting))
  const payload = transformFormDataToUpdatePayload(form, 7)
  expect(JSON.parse(payload.setting ?? '{}')).toMatchObject(storedSetting)
})

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
