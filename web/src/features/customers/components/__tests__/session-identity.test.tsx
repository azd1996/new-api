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

import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'

import {
  CUSTOMER_FORM_DEFAULT_VALUES,
  transformFormDataToPayload,
  type CustomerFormValues,
} from '../../lib/customer-form'
import { customerSchema, type Customer } from '../../types'
import { CustomerSessionIdentity } from '../customer-session-identity'

const { testSessionIdentityMock } = vi.hoisted(() => ({
  testSessionIdentityMock: vi.fn(),
}))

vi.mock('../../api', () => ({
  testCustomerSessionIdentity: testSessionIdentityMock,
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

function SessionIdentityForm(props: {
  defaults?: CustomerFormValues
  onSave: (value: unknown) => void
}) {
  const form = useForm<CustomerFormValues>({
    defaultValues:
      props.defaults ??
      ({
        ...CUSTOMER_FORM_DEFAULT_VALUES,
        customer_id: customer.customer_id,
      } as CustomerFormValues),
  })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return (
    <QueryClientProvider client={queryClient}>
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit((values) =>
            props.onSave(transformFormDataToPayload(values))
          )}
        >
          <CustomerSessionIdentity customerId={customer.customer_id} />
          <Button type='submit'>Save</Button>
        </form>
      </Form>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  testSessionIdentityMock.mockReset()
})

afterEach(() => {
  vi.clearAllMocks()
})

it('keeps inheritance when a customer without session identity is saved', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(<SessionIdentityForm onSave={onSave} />)

  expect(
    screen.queryByRole('switch', { name: 'Enable extraction' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
  expect(onSave.mock.calls[0][0]).not.toHaveProperty('session_identity')
})

it('customizes and saves an enabled regex source with default policies', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(<SessionIdentityForm onSave={onSave} />)

  await user.click(
    screen.getByRole('button', { name: 'Customize session identity' })
  )
  const enabled = screen.getByRole('switch', { name: 'Enable extraction' })
  expect(enabled).not.toBeChecked()

  await user.click(enabled)
  await user.click(screen.getByRole('button', { name: 'Add source' }))
  fireEvent.change(screen.getByLabelText('Pattern'), {
    target: { value: '^\\[([^\\]]+)-fixed-\\d+\\]' },
  })
  await user.click(screen.getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
  expect(onSave.mock.calls[0][0]).toMatchObject({
    session_identity: {
      enabled: true,
      model_prefixes: ['claude-'],
      sources: [
        {
          type: 'regex',
          path: 'system.0.text',
          pattern: '^\\[([^\\]]+)-fixed-\\d+\\]',
          capture_group: 1,
        },
      ],
      transform: 'xxhash64',
      on_empty: 'legacy',
      disable_first_message_fallback: false,
      ttl_seconds: 0,
    },
  })
})

it('runs the dry run on pasted request bodies and reports stability', async () => {
  testSessionIdentityMock.mockResolvedValue({
    results: [
      {
        index: 0,
        ok: true,
        session_id: 'b3dd1a5708f87b4b',
        raw_value: 'marker',
        source: 'custom:regex(system.0.text)',
      },
      {
        index: 1,
        ok: true,
        session_id: 'b3dd1a5708f87b4b',
        raw_value: 'marker',
        source: 'custom:regex(system.0.text)',
      },
    ],
    summary: {
      count: 2,
      ok_count: 2,
      distinct_session_ids: 1,
      session_ids: ['b3dd1a5708f87b4b'],
      stable: true,
      expect_same_met: true,
      sources_used: { 'custom:regex(system.0.text)': 2 },
    },
  })
  const user = userEvent.setup()
  render(<SessionIdentityForm onSave={vi.fn()} />)

  await user.click(
    screen.getByRole('button', { name: 'Customize session identity' })
  )
  await user.click(screen.getByRole('switch', { name: 'Enable extraction' }))
  const textarea = screen.getByLabelText('Request bodies')
  fireEvent.change(textarea, {
    target: { value: '{"model":"m1"}\n{"model":"m2"}' },
  })
  await user.click(screen.getByRole('button', { name: 'Test extraction' }))

  await waitFor(() => expect(testSessionIdentityMock).toHaveBeenCalledTimes(1))
  expect(testSessionIdentityMock.mock.calls[0][0].items).toHaveLength(2)
  expect(testSessionIdentityMock.mock.calls[0][0].config).toMatchObject({
    enabled: true,
  })
  expect(await screen.findByText('Stable')).toBeVisible()
  expect(screen.getAllByText('b3dd1a5708f87b4b')).toHaveLength(2)
})

it('hydrates a saved configuration and preserves it when other fields change', async () => {
  const saved = customerSchema.parse({
    ...customer,
    session_identity: {
      enabled: true,
      model_prefixes: null,
      sources: [
        {
          type: 'request_header',
          key: 'x-session-id',
          json_string_field: '',
        },
      ],
      transform: '',
      value_regex: '',
      on_empty: 'stable',
      disable_first_message_fallback: true,
      ttl_seconds: 0,
    },
  })
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(
    <SessionIdentityForm
      defaults={{
        ...CUSTOMER_FORM_DEFAULT_VALUES,
        customer_id: saved.customer_id,
        session_identity: {
          enabled: true,
          model_prefixes: [],
          sources: [
            {
              type: 'request_header',
              key: 'x-session-id',
              path: '',
              json_string_field: '',
              pattern: '',
              capture_group: 0,
            },
          ],
          transform: '',
          value_regex: '',
          on_empty: 'stable',
          disable_first_message_fallback: true,
          ttl_seconds: 0,
        },
      }}
      onSave={onSave}
    />
  )

  const enabled = screen.getByRole('switch', { name: 'Enable extraction' })
  expect(enabled).toBeChecked()
  expect(screen.getByLabelText('Header name')).toHaveValue('x-session-id')

  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
  expect(onSave.mock.calls[0][0]).toMatchObject({
    session_identity: {
      enabled: true,
      model_prefixes: [],
      sources: [
        {
          type: 'request_header',
          key: 'x-session-id',
        },
      ],
      on_empty: 'stable',
      disable_first_message_fallback: true,
    },
  })
})
