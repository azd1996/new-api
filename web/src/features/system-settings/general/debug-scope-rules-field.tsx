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
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import type { DebugScopeRule } from '../types'

function toRules(rows: DebugScopeRule[]): DebugScopeRule[] {
  return rows.map((row) => ({
    group: (row.group ?? '').trim(),
    model: (row.model ?? '').trim(),
  }))
}

type DebugScopeRulesFieldProps = {
  label: string
  description: string
  value: DebugScopeRule[]
  onChange: (next: DebugScopeRule[]) => void
}

export function DebugScopeRulesField(props: DebugScopeRulesFieldProps) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<DebugScopeRule[]>(() => [...props.value])
  const hasLegacyRules = props.value.some(
    (rule) => 'token_name' in rule && !rule.group
  )
  // Tracks the serialized rules we last emitted so an external value change
  // (save/reset) resyncs the rows, while echoes of our own onChange do not.
  const lastEmittedRef = useRef(JSON.stringify(props.value))

  useEffect(() => {
    const incoming = JSON.stringify(props.value)
    if (incoming === lastEmittedRef.current) return
    lastEmittedRef.current = incoming
    setRows([...props.value])
  }, [props.value])

  const commit = (next: DebugScopeRule[]) => {
    setRows(next)
    const rules = toRules(next)
    lastEmittedRef.current = JSON.stringify(rules)
    props.onChange(rules)
  }

  const updateRow = (index: number, patch: Partial<DebugScopeRule>) => {
    commit(rows.map((row, i) => (i === index ? { ...row, ...patch } : row)))
  }

  return (
    <div className='space-y-2'>
      <div className='space-y-0.5'>
        <Label className='text-sm font-medium'>{props.label}</Label>
        <p className='text-muted-foreground text-xs'>{props.description}</p>
      </div>
      {hasLegacyRules && (
        <Alert>
          <AlertDescription>
            {t(
              'Legacy API key debug rules are disabled. Enter a routing group for each rule and save to enable group-scoped logging.'
            )}
          </AlertDescription>
        </Alert>
      )}
      {rows.length === 0 ? (
        <p className='text-muted-foreground text-xs'>{t('No rules yet')}</p>
      ) : (
        <div className='space-y-2'>
          {rows.map((row, index) => (
            // Row identity is positional for this editable free-text list;
            // content can be empty/duplicate mid-edit so index is the stable key.
            // eslint-disable-next-line react/no-array-index-key
            <div key={index} className='flex items-start gap-2'>
              <Input
                className='min-w-0 flex-1'
                placeholder={t('Group name or prefix (e.g. team-*)')}
                aria-label={t('Group name or prefix (e.g. team-*)')}
                value={row.group ?? ''}
                onChange={(event) =>
                  updateRow(index, { group: event.target.value })
                }
              />
              <Input
                className='min-w-0 flex-1'
                placeholder={t('Model name or prefix (e.g. gpt-4o*)')}
                aria-label={t('Model name or prefix (e.g. gpt-4o*)')}
                value={row.model ?? ''}
                onChange={(event) =>
                  updateRow(index, { model: event.target.value })
                }
              />
              <Button
                type='button'
                variant='ghost'
                size='icon'
                className='h-9 w-9 shrink-0'
                onClick={() => commit(rows.filter((_, i) => i !== index))}
                aria-label={t('Delete')}
              >
                <Trash2 className='h-4 w-4' />
              </Button>
            </div>
          ))}
        </div>
      )}
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => commit([...rows, { group: '', model: '' }])}
      >
        <Plus className='mr-1 h-3 w-3' />
        {t('Add Rule')}
      </Button>
    </div>
  )
}
