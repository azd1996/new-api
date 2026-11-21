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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { AIGeneratedRule, ErrorStatItem, SignatureRule } from '../../types'
import { ErrorSignatureAIRuleDialog } from '../error-signature-ai-rule-dialog'

const item: ErrorStatItem = {
  group_key: 'hash-1',
  group_label: 'ValidationException: invalid signature',
  signature: 'ValidationException: invalid signature',
  signature_hash: 'hash-1',
  short_name: '',
  attribution: '',
  note: '',
  count: 3,
  first_seen: 0,
  last_seen: 0,
  status_code: 400,
  models: [],
  channels: [],
  customers: [],
}

const result: AIGeneratedRule = {
  rule: {
    name: 'invalid-thinking-signature',
    pattern: 'ValidationException: invalid signature in request',
    signature: 'ValidationException: invalid signature',
    attribution: '客户端问题-可解决',
    note: 'invalid thinking signature',
    enabled: true,
  },
  samples: [
    'status_code=400, ValidationException: invalid signature in request abc123',
  ],
  matched_sample: true,
}

function renderDialog(
  overrides?: Partial<{
    generating: boolean
    result: AIGeneratedRule | null
    error: string | null
    onAddRule: (rule: SignatureRule) => void
    onRegenerate: () => void
  }>
) {
  const onAddRule = vi.fn()
  const onRegenerate = vi.fn()
  render(
    <ErrorSignatureAIRuleDialog
      open
      onOpenChange={vi.fn()}
      item={item}
      generating={false}
      result={result}
      error={null}
      attributionOptions={['客户端问题-可解决', '源站问题']}
      onRegenerate={onRegenerate}
      onAddRule={onAddRule}
      {...overrides}
    />
  )
  return { onAddRule, onRegenerate }
}

describe('ErrorSignatureAIRuleDialog', () => {
  it('shows the generated rule, verifies it against the raw sample, and submits edits', async () => {
    const { onAddRule } = renderDialog()

    expect(screen.getByDisplayValue('invalid-thinking-signature')).toBeVisible()
    expect(
      screen.getByDisplayValue(
        'ValidationException: invalid signature in request'
      )
    ).toBeVisible()
    expect(screen.getByText('Pattern matches the sample error.')).toBeVisible()
    expect(
      screen.getByText(
        'status_code=400, ValidationException: invalid signature in request abc123'
      )
    ).toBeVisible()

    await userEvent.type(screen.getByLabelText('Rule name'), '-v2')
    await userEvent.click(screen.getByRole('button', { name: 'Add to rules' }))

    expect(onAddRule).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'invalid-thinking-signature-v2',
        pattern: 'ValidationException: invalid signature in request',
        signature: 'ValidationException: invalid signature',
        attribution: '客户端问题-可解决',
        enabled: true,
      })
    )
  })

  it('reports an edited pattern that no longer matches the raw sample', async () => {
    renderDialog()

    const pattern = screen.getByLabelText('Pattern')
    await userEvent.clear(pattern)
    await userEvent.type(pattern, 'a-different-error')

    expect(
      screen.getByText('Pattern does not match the sample error.')
    ).toBeVisible()
  })

  it('shows the generating state and disables adding until the model responds', () => {
    const { onRegenerate } = renderDialog({
      generating: true,
      result: null,
    })

    expect(screen.getByText('Asking the AI assistant...')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Generating...' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Add to rules' })).toBeDisabled()
    expect(onRegenerate).not.toHaveBeenCalled()
  })

  it('shows an inline error and allows regenerating after a failure', async () => {
    const { onRegenerate } = renderDialog({
      result: null,
      error: 'AI assistant is not enabled',
    })

    expect(screen.getByText('AI assistant is not enabled')).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Regenerate' }))
    expect(onRegenerate).toHaveBeenCalledTimes(1)
  })
})
