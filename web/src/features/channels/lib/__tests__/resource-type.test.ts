import { expect, test } from 'vitest'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'
import { channelSchema } from '../../types'

test('selected resource type is retained when loading and saving a channel', () => {
  const form = channelFormSchema.parse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Claude',
    type: 14,
    key: 'test-key',
    models: 'claude-sonnet',
    resource_type_id: 12,
  })
  const createPayload = transformFormDataToCreatePayload(form).channel
  const channel = channelSchema.parse({
    ...createPayload,
    other: '',
    remark: '',
    id: 7,
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })

  expect(transformChannelToFormDefaults(channel).resource_type_id).toBe(12)
  expect(transformFormDataToUpdatePayload(form, 7).resource_type_id).toBe(12)
})

test('clearing a channel resource type sends an explicit null update', () => {
  const form = channelFormSchema.parse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Claude',
    type: 14,
    key: '',
    models: 'claude-sonnet',
    resource_type_id: null,
  })

  expect(transformFormDataToUpdatePayload(form, 7).resource_type_id).toBeNull()
})
