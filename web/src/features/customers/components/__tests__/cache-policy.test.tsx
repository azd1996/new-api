import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { afterEach, expect, it, vi } from 'vitest'

import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'

import {
  CUSTOMER_FORM_DEFAULT_VALUES,
  transformCustomerToFormDefaults,
  transformFormDataToPayload,
  type CustomerFormValues,
} from '../../lib/customer-form'
import { customerSchema, type Customer } from '../../types'
import { CustomerCachePolicy } from '../customer-cache-policy'
import { CustomerSessionRoutingPolicy } from '../customer-session-routing-policy'

afterEach(cleanup)

const customer: Customer = {
  id: 1,
  customer_id: 'customer-1',
  name: 'Original',
  remark: '',
  prefix_discounts: {},
  created_time: 0,
  updated_time: 0,
}

function PolicyForm(props: {
  defaults?: CustomerFormValues
  onSave: (value: unknown) => void
}) {
  const form = useForm<CustomerFormValues>({
    defaultValues: props.defaults ?? {
      ...CUSTOMER_FORM_DEFAULT_VALUES,
      customer_id: 'customer-1',
    },
  })
  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((values) =>
          props.onSave(transformFormDataToPayload(values))
        )}
      >
        <input aria-label='Customer Name' {...form.register('name')} />
        <CustomerCachePolicy />
        <CustomerSessionRoutingPolicy />
        <Button type='submit'>Save</Button>
      </form>
    </Form>
  )
}

it.each([undefined, null])(
  'keeps inheritance when a legacy customer with policy %s is only renamed',
  async (policy) => {
    const user = userEvent.setup()
    const onSave = vi.fn()
    render(
      <PolicyForm
        defaults={transformCustomerToFormDefaults({
          ...customer,
          claude_cache_optimization: policy,
        })}
        onSave={onSave}
      />
    )
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    await user.clear(screen.getByRole('textbox', { name: 'Customer Name' }))
    await user.type(
      screen.getByRole('textbox', { name: 'Customer Name' }),
      'Renamed'
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'Renamed' })
      )
    )
    expect(onSave.mock.calls[0][0]).not.toHaveProperty(
      'claude_cache_optimization'
    )
  }
)

it('customizing saves five independent rules and keeps created prefixes when switched off', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(<PolicyForm onSave={onSave} />)
  await user.click(
    screen.getByRole('button', { name: 'Customize cache rules' })
  )
  expect(screen.getAllByRole('switch')).toHaveLength(5)
  for (const control of screen.getAllByRole('switch')) {
    expect(control).not.toBeChecked()
  }
  for (const input of screen.getAllByRole('combobox')) {
    expect(input).toBeDisabled()
  }
  const extended = screen.getByRole('switch', { name: 'Extended cache TTL' })
  await user.click(extended)
  const inputs = screen.getAllByRole('combobox')
  await user.type(inputs[0], 'custom-claude-,')
  await user.keyboard('{Escape}')
  await user.click(extended)
  expect(inputs[0]).toBeDisabled()
  await user.click(
    screen.getByRole('switch', { name: 'Stable base breakpoint' })
  )
  await user.click(
    screen.getByRole('switch', { name: 'Fallback cache breakpoint' })
  )
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(onSave).toHaveBeenCalled())
  expect(onSave.mock.calls[0][0]).toMatchObject({
    claude_cache_optimization: {
      force_extended_ttl: {
        enabled: false,
        model_prefixes: ['custom-claude-'],
      },
      fix_volatile_breakpoint: { enabled: false, model_prefixes: [] },
      retain_previous_breakpoint: { enabled: false, model_prefixes: [] },
      stable_base_breakpoint: { enabled: true, model_prefixes: [] },
      fallback_cache_breakpoint: { enabled: true, model_prefixes: [] },
    },
  })
})

it('hydrates an old four-rule policy with fallback disabled and normalizes null prefixes without losing enabled rules', () => {
  const parsed = customerSchema.parse({
    ...customer,
    claude_cache_optimization: {
      force_extended_ttl: { enabled: true, model_prefixes: null },
      fix_volatile_breakpoint: {
        enabled: false,
        model_prefixes: ['claude-opus-'],
      },
      retain_previous_breakpoint: { enabled: true, model_prefixes: [] },
      stable_base_breakpoint: { enabled: false, model_prefixes: [] },
    },
  })
  const payload = transformFormDataToPayload(
    transformCustomerToFormDefaults(parsed)
  )
  expect(payload.claude_cache_optimization).toEqual({
    force_extended_ttl: { enabled: true, model_prefixes: [] },
    fix_volatile_breakpoint: {
      enabled: false,
      model_prefixes: ['claude-opus-'],
    },
    retain_previous_breakpoint: { enabled: true, model_prefixes: [] },
    stable_base_breakpoint: { enabled: false, model_prefixes: [] },
    fallback_cache_breakpoint: { enabled: false, model_prefixes: [] },
  })
})

it('defaults legacy customers to disabled session routing and preserves model-prefix rules', () => {
  const legacy = transformCustomerToFormDefaults(customer)
  expect(legacy.session_routing_model_prefixes).toEqual({})

  const configured = transformCustomerToFormDefaults({
    ...customer,
    session_routing_model_prefixes: { 'claude-': true, 'gpt-': false },
  })
  expect(configured.session_routing_model_prefixes).toEqual({
    'claude-': true,
    'gpt-': false,
  })
  expect(transformFormDataToPayload(configured)).toMatchObject({
    session_routing_model_prefixes: {
      'claude-': true,
      'gpt-': false,
    },
  })
})

it('removes a model prefix rule without changing the other rules', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  render(
    <PolicyForm
      defaults={transformCustomerToFormDefaults({
        ...customer,
        session_routing_model_prefixes: {
          'claude-': true,
          'gpt-': false,
        },
      })}
      onSave={onSave}
    />
  )

  expect(screen.getByText('claude-')).toBeVisible()
  expect(screen.getByText('gpt-')).toBeVisible()
  await user.click(screen.getAllByRole('button', { name: 'Remove' })[0])

  expect(screen.getByRole('alertdialog')).toBeVisible()
  expect(screen.getByText('Are you sure?')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(screen.getByText('claude-')).toBeVisible()

  await user.click(screen.getAllByRole('button', { name: 'Remove' })[0])
  await user.click(screen.getByRole('button', { name: 'Confirm delete' }))
  expect(screen.queryByText('claude-')).not.toBeInTheDocument()
  expect(screen.getByText('gpt-')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        session_routing_model_prefixes: { 'gpt-': false },
      })
    )
  )
})

it('keeps fallback prefixes through toggling, saving and reopening without changing other rules', async () => {
  const user = userEvent.setup()
  const onSave = vi.fn()
  const view = render(<PolicyForm onSave={onSave} />)
  await user.click(
    screen.getByRole('button', { name: 'Customize cache rules' })
  )
  const fallback = screen.getByRole('switch', {
    name: 'Fallback cache breakpoint',
  })
  await user.click(fallback)
  expect(fallback).toBeChecked()
  const prefixes = screen.getAllByRole('combobox')[4]
  expect(prefixes).toBeEnabled()
  expect(prefixes).toHaveAttribute('placeholder', 'All supported models')
  await user.type(prefixes, 'claude-opus-,custom-claude-,')
  await user.keyboard('{Escape}')
  await user.click(fallback)
  expect(prefixes).toBeDisabled()
  await user.click(fallback)
  expect(prefixes).toBeEnabled()
  expect(screen.getByText('custom-claude-')).toBeVisible()
  await user.click(fallback)
  await user.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(onSave).toHaveBeenCalled())
  const saved = customerSchema.parse({
    ...customer,
    ...onSave.mock.calls[0][0],
  })
  expect(saved.claude_cache_optimization).toEqual({
    force_extended_ttl: { enabled: false, model_prefixes: [] },
    fix_volatile_breakpoint: { enabled: false, model_prefixes: [] },
    retain_previous_breakpoint: { enabled: false, model_prefixes: [] },
    stable_base_breakpoint: { enabled: false, model_prefixes: [] },
    fallback_cache_breakpoint: {
      enabled: false,
      model_prefixes: ['claude-opus-', 'custom-claude-'],
    },
  })
  view.unmount()
  render(
    <PolicyForm
      defaults={transformCustomerToFormDefaults(saved)}
      onSave={onSave}
    />
  )
  expect(
    screen.getByRole('switch', { name: 'Fallback cache breakpoint' })
  ).not.toBeChecked()
  expect(screen.getAllByRole('combobox')[4]).toBeDisabled()
  expect(screen.getByText('custom-claude-')).toBeVisible()
})
