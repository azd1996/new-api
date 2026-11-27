import { fireEvent, render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import type { DebugScopeRule } from '../../types'
import { DebugScopeRulesField } from '../debug-scope-rules-field'

test('editing a group rule emits group and model without token metadata', () => {
  const onChange = vi.fn()
  render(
    <DebugScopeRulesField
      label='Debug scopes'
      description=''
      value={[{ group: 'team-a', model: 'claude-*' }]}
      onChange={onChange}
    />
  )
  fireEvent.change(screen.getByDisplayValue('team-a'), {
    target: { value: ' team-* ' },
  })
  expect(onChange).toHaveBeenLastCalledWith([
    { group: 'team-*', model: 'claude-*' },
  ])
})

test('legacy key rules show a warning and require an explicit group', () => {
  const legacy = JSON.parse(
    '[{"token_name":"old-key","model":"claude-*"}]'
  ) as DebugScopeRule[]
  const onChange = vi.fn()
  render(
    <DebugScopeRulesField
      label='Debug scopes'
      description=''
      value={legacy}
      onChange={onChange}
    />
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    /Legacy API key debug rules/
  )
  const group = screen.getByRole('textbox', { name: /Group name or prefix/ })
  expect(group).toHaveValue('')
  expect(onChange).not.toHaveBeenCalled()
  fireEvent.change(screen.getByDisplayValue('claude-*'), {
    target: { value: 'gpt-*' },
  })
  expect(onChange).toHaveBeenLastCalledWith([{ group: '', model: 'gpt-*' }])
  fireEvent.change(group, { target: { value: 'team-a' } })
  expect(onChange).toHaveBeenLastCalledWith([
    { group: 'team-a', model: 'gpt-*' },
  ])
})

test('adding and removing a rule keeps empty fields explicit and can disable scopes', () => {
  const onChange = vi.fn()
  render(
    <DebugScopeRulesField
      label='Debug scopes'
      description=''
      value={[]}
      onChange={onChange}
    />
  )
  fireEvent.click(screen.getByRole('button', { name: /Add Rule/ }))
  expect(onChange).toHaveBeenLastCalledWith([{ group: '', model: '' }])
  fireEvent.click(screen.getByRole('button', { name: /Delete/ }))
  expect(onChange).toHaveBeenLastCalledWith([])
})

test('external reset replaces edited groups without losing model values', () => {
  const onChange = vi.fn()
  const props = { label: 'Debug scopes', description: '', onChange }
  const view = render(
    <DebugScopeRulesField
      {...props}
      value={[{ group: 'team-a', model: '*' }]}
    />
  )
  fireEvent.change(screen.getByDisplayValue('team-a'), {
    target: { value: 'team-b' },
  })
  view.rerender(
    <DebugScopeRulesField
      {...props}
      value={[{ group: 'team-c', model: 'gpt-*' }]}
    />
  )
  expect(screen.getByDisplayValue('team-c')).toBeInTheDocument()
  expect(screen.getByDisplayValue('gpt-*')).toBeInTheDocument()
})
