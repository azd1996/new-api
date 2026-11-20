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

import { DEFAULT_CONFIG, DEFAULT_PARAMETER_ENABLED } from '../../../constants'
import type { Message } from '../../../types'
import { buildChatCompletionPayload } from '../payload-builder'

const message: Message = {
  key: 'a-chat',
  from: 'user',
  versions: [{ id: 'a-version', content: 'Hello' }],
}

test('buildChatCompletionPayload prepends a provided system prompt', () => {
  const payload = buildChatCompletionPayload(
    [message],
    DEFAULT_CONFIG,
    DEFAULT_PARAMETER_ENABLED,
    'You are a helpful assistant'
  )

  expect(payload.messages).toHaveLength(2)
  expect(payload.messages[0]).toEqual({
    role: 'system',
    content: 'You are a helpful assistant',
  })
  expect(payload.messages[1]).toEqual({ role: 'user', content: 'Hello' })
})

test('buildChatCompletionPayload omits a blank system prompt', () => {
  const payload = buildChatCompletionPayload(
    [message],
    DEFAULT_CONFIG,
    DEFAULT_PARAMETER_ENABLED,
    '   '
  )

  expect(payload.messages).toHaveLength(1)
  expect(payload.messages[0]).toEqual({ role: 'user', content: 'Hello' })
})

test('buildChatCompletionPayload works without a system prompt', () => {
  const payload = buildChatCompletionPayload(
    [message],
    DEFAULT_CONFIG,
    DEFAULT_PARAMETER_ENABLED
  )

  expect(payload.messages).toHaveLength(1)
  expect(payload.messages[0]).toEqual({ role: 'user', content: 'Hello' })
})
