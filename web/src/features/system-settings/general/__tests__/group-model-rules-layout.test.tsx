import { render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { GroupModelRulesField } from '../group-model-rules-field'

test('group rules cap the widened group input and leave room for models', () => {
  render(
    <GroupModelRulesField
      label='Force 1h Prompt Cache TTL'
      description=''
      value={[{ group: 'customer-routing-group', models: ['claude-sonnet'] }]}
      onChange={vi.fn()}
    />
  )

  expect(screen.getByDisplayValue('customer-routing-group')).toHaveClass(
    'w-120',
    'max-w-[60%]'
  )
  expect(screen.getByDisplayValue('claude-sonnet')).toHaveClass(
    'min-w-0',
    'flex-1'
  )
})
