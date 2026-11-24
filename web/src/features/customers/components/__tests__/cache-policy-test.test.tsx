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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { Form } from '@/components/ui/form'

import {
  CUSTOMER_FORM_DEFAULT_VALUES,
  type CustomerFormValues,
} from '../../lib/customer-form'
import { customerSchema, type Customer } from '../../types'
import { CustomerCachePolicyTest } from '../customer-cache-policy-test'

const { testCustomerCacheOptimizationMock, toastErrorMock, getGroupsMock } =
  vi.hoisted(() => ({
    testCustomerCacheOptimizationMock: vi.fn(),
    toastErrorMock: vi.fn(),
    getGroupsMock: vi.fn(),
  }))

vi.mock('../../api', () => ({
  testCustomerCacheOptimization: testCustomerCacheOptimizationMock,
}))

vi.mock('@/features/users/api', () => ({
  getGroups: getGroupsMock,
}))

vi.mock('sonner', () => ({
  toast: { error: toastErrorMock },
}))

const customer: Customer = customerSchema.parse({
  id: 1,
  customer_id: 'customer-1',
  name: 'Original',
  remark: '',
  prefix_discounts: {},
  created_time: 0,
  updated_time: 0,
})

const policy = customerSchema.parse({
  ...customer,
  claude_cache_optimization: {
    force_extended_ttl: { enabled: true, model_prefixes: [] },
    fix_volatile_breakpoint: { enabled: false, model_prefixes: [] },
    retain_previous_breakpoint: { enabled: false, model_prefixes: [] },
    stable_base_breakpoint: { enabled: false, model_prefixes: [] },
    fallback_cache_breakpoint: { enabled: false, model_prefixes: [] },
  },
}).claude_cache_optimization ?? null

function TestForm(props: {
  policy: CustomerFormValues['claude_cache_optimization']
}) {
  const form = useForm<CustomerFormValues>({
    defaultValues: {
      ...CUSTOMER_FORM_DEFAULT_VALUES,
      customer_id: customer.customer_id,
      claude_cache_optimization: props.policy,
    },
  })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return (
    <QueryClientProvider client={queryClient}>
      <Form {...form}>
        <CustomerCachePolicyTest customerId={customer.customer_id} />
      </Form>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  testCustomerCacheOptimizationMock.mockReset()
  toastErrorMock.mockReset()
  getGroupsMock.mockReset()
  getGroupsMock.mockResolvedValue({
    success: true,
    message: '',
    data: ['default', 'vip-group-with-a-very-long-name-for-hovering'],
  })
})

afterEach(() => {
  vi.clearAllMocks()
})

it('runs the dry run with the in-form policy and renders the processed body', async () => {
  testCustomerCacheOptimizationMock.mockResolvedValue({
    results: [
      {
        index: 0,
        ok: true,
        model: 'claude-sonnet-4',
        body: '{"model":"claude-sonnet-4","messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}',
        enabled_rules: ['force_extended_ttl'],
        changed_steps: ['force_extended_ttl'],
        headers: { 'anthropic-beta': 'extended-cache-ttl-2025-04-11' },
      },
    ],
  })
  const user = userEvent.setup()
  render(<TestForm policy={policy} />)

  fireEvent.change(screen.getByLabelText('Request bodies'), {
    target: { value: '{"model":"claude-sonnet-4","messages":[]}' },
  })
  await user.click(screen.getByRole('button', { name: 'Test optimizations' }))

  await waitFor(() =>
    expect(testCustomerCacheOptimizationMock).toHaveBeenCalledTimes(1)
  )
  expect(testCustomerCacheOptimizationMock.mock.calls[0][0]).toMatchObject({
    customer_id: 'customer-1',
    group: 'default',
    items: [{ body: '{"model":"claude-sonnet-4","messages":[]}' }],
    config: policy,
  })
  expect(screen.getByText('#1 · claude-sonnet-4')).toBeVisible()
  expect(screen.getAllByText('Extended cache TTL')).toHaveLength(2)
  expect(
    screen.getByText(/anthropic-beta: extended-cache-ttl-2025-04-11/)
  ).toBeVisible()
  expect(screen.getByText(/"ttl": "1h"/)).toBeVisible()
})

it('does not run the dry run without a valid request body', async () => {
  const user = userEvent.setup()
  render(<TestForm policy={null} />)

  await user.click(screen.getByRole('button', { name: 'Test optimizations' }))

  expect(testCustomerCacheOptimizationMock).not.toHaveBeenCalled()
  expect(toastErrorMock).toHaveBeenCalledWith(
    'Paste at least one valid JSON request body'
  )
})

it('runs the full-relay preview and shows routing details', async () => {
  testCustomerCacheOptimizationMock.mockResolvedValue({
    mode: 'full_relay',
    results: [
      {
        index: 0,
        ok: true,
        model: 'claude-sonnet-4',
        body: '{"model":"claude-3-5-sonnet","messages":[]}',
        channel_id: 7,
        channel_name: 'Anthropic',
        channel_type: 14,
        group: 'default',
        origin_model: 'claude-sonnet-4',
        upstream_model: 'claude-3-5-sonnet',
        model_mapped: true,
        url: 'https://api.anthropic.com/v1/messages',
        method: 'POST',
        headers: {
          Accept: 'text/event-stream',
          'X-Api-Key': '***',
        },
        enabled_rules: ['force_extended_ttl'],
        skipped_steps: ['retain_previous_breakpoint'],
      },
    ],
  })
  const user = userEvent.setup()
  render(<TestForm policy={null} />)

  fireEvent.change(screen.getByLabelText('Request bodies'), {
    target: { value: '{"model":"claude-sonnet-4","messages":[]}' },
  })
  fireEvent.change(screen.getByLabelText('Test mode'), {
    target: { value: 'full_relay' },
  })
  await user.click(screen.getByRole('button', { name: 'Test optimizations' }))

  await waitFor(() =>
    expect(testCustomerCacheOptimizationMock).toHaveBeenCalledTimes(1)
  )
  expect(testCustomerCacheOptimizationMock.mock.calls[0][0].mode).toBe(
    'full_relay'
  )
  expect(screen.getByText('Channel: Anthropic (#7)')).toBeVisible()
  expect(
    screen.getByText(/Model: claude-sonnet-4 → claude-3-5-sonnet \(mapped\)/)
  ).toBeVisible()
  expect(
    screen.getByText('Upstream URL: POST https://api.anthropic.com/v1/messages')
  ).toBeVisible()
  expect(
    screen.getByText('Retain previous breakpoint (requires upstream cache state)')
  ).toBeVisible()
  expect(
    screen.getByText(/Outbound headers \(redacted\):/)
  ).toBeVisible()
})

it('pre-fills editable client headers and sends them in the full-relay mode', async () => {
  testCustomerCacheOptimizationMock.mockResolvedValue({
    mode: 'full_relay',
    results: [{ index: 0, ok: true, body: '{}', url: 'https://u', method: 'POST' }],
  })
  const user = userEvent.setup()
  render(<TestForm policy={null} />)

  fireEvent.change(screen.getByLabelText('Request bodies'), {
    target: { value: '{"model":"claude-sonnet-4","messages":[]}' },
  })
  fireEvent.change(screen.getByLabelText('Test mode'), {
    target: { value: 'full_relay' },
  })

  const headersEditor = screen.getByLabelText('Simulated client headers')
  expect(headersEditor).toHaveValue(
    'Accept: text/event-stream\nAnthropic-Beta: context-management-2025-06-27\nAnthropic-Version: 2023-06-01\nContent-Type: application/json'
  )
  // Edit one default header and add a custom one.
  fireEvent.change(headersEditor, {
    target: {
      value:
        'Anthropic-Version: 2023-06-01\nX-Custom-Trace: abc\nContent-Type: application/json',
    },
  })
  await user.click(screen.getByRole('button', { name: 'Test optimizations' }))

  await waitFor(() =>
    expect(testCustomerCacheOptimizationMock).toHaveBeenCalledTimes(1)
  )
  expect(testCustomerCacheOptimizationMock.mock.calls[0][0].items[0].headers).toEqual({
    'Anthropic-Version': '2023-06-01',
    'X-Custom-Trace': 'abc',
    'Content-Type': 'application/json',
  })
})

it('blocks the full-relay test when a header line is malformed', async () => {
  const user = userEvent.setup()
  render(<TestForm policy={null} />)

  fireEvent.change(screen.getByLabelText('Request bodies'), {
    target: { value: '{"model":"claude-sonnet-4","messages":[]}' },
  })
  fireEvent.change(screen.getByLabelText('Test mode'), {
    target: { value: 'full_relay' },
  })
  fireEvent.change(screen.getByLabelText('Simulated client headers'), {
    target: { value: 'no-colon-line' },
  })
  await user.click(screen.getByRole('button', { name: 'Test optimizations' }))

  expect(testCustomerCacheOptimizationMock).not.toHaveBeenCalled()
  expect(toastErrorMock).toHaveBeenCalledWith(
    expect.stringContaining('Invalid header lines')
  )
})

it('offers groups in a dropdown and keeps the full name available on hover', async () => {
  const user = userEvent.setup()
  render(<TestForm policy={null} />)

  await user.click(screen.getByLabelText('Group'))

  const longGroupName = 'vip-group-with-a-very-long-name-for-hovering'
  const option = await screen.findByRole('option', { name: longGroupName })
  expect(option).toBeVisible()
  expect(option).toHaveAttribute('title', longGroupName)
  expect(screen.getByRole('option', { name: 'default' })).toHaveAttribute(
    'title',
    'default'
  )
})
