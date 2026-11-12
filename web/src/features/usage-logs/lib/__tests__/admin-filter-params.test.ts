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
import { describe, expect, test } from 'vitest'

import { buildApiParams } from '../utils'

const adminSearch = {
  downstreamTraceTag: 'tag-1',
  customerName: 'acme',
  retryTriggered: 'true',
  node: 'node-a,node-b',
  nodeOp: 'exclude',
  sessionIdSource: 'prompt_cache_key,metadata.user_id',
  sessionIdSourceOp: 'exclude',
  retryRule: 'case2, a',
  retryRuleOp: 'exclude',
}

describe('usage log admin filter params', () => {
  test('sends trace tag, customer name, node, session id source and retry flag for admin views', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams: adminSearch,
      isAdmin: true,
    })

    expect(params).toMatchObject({
      downstream_trace_tag: 'tag-1',
      customer_name: 'acme',
      retry_triggered: 'true',
      node_name: 'node-a,node-b',
      node_name_op: 'exclude',
      session_id_source: 'prompt_cache_key,metadata.user_id',
      session_id_source_op: 'exclude',
      retry_rule: 'case2, a',
      retry_rule_op: 'exclude',
    })
  })

  test('drops admin-only filters for non-admin views', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams: adminSearch,
      isAdmin: false,
    })

    expect(params.downstream_trace_tag).toBeUndefined()
    expect(params.customer_name).toBeUndefined()
    expect(params.retry_triggered).toBeUndefined()
    expect(params.node_name).toBeUndefined()
    expect(params.node_name_op).toBeUndefined()
    expect(params.session_id_source).toBeUndefined()
    expect(params.session_id_source_op).toBeUndefined()
    expect(params.retry_rule).toBeUndefined()
    expect(params.retry_rule_op).toBeUndefined()
  })

  test('ignores a retry flag that is neither true nor false', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams: { retryTriggered: 'maybe' },
      isAdmin: true,
    })

    expect(params.retry_triggered).toBeUndefined()
  })

  test('sends the retry rule keyword without an operator by default', () => {
    const params = buildApiParams({
      page: 1,
      pageSize: 20,
      searchParams: { retryRule: 'case1', retryRuleOp: 'include' },
      isAdmin: true,
    })

    expect(params.retry_rule).toBe('case1')
    expect(params.retry_rule_op).toBeUndefined()
  })
})
