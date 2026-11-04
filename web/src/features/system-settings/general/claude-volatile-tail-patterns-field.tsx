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
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { FormDescription, FormLabel } from '@/components/ui/form'

const defaultPatternOptions = ['<total_tokens>'].map((value) => ({
  value,
  label: value,
}))

type ClaudeVolatileTailPatternsFieldProps = {
  value: string[]
  onChange: (next: string[]) => void
}

export function ClaudeVolatileTailPatternsField(
  props: ClaudeVolatileTailPatternsFieldProps
) {
  const { t } = useTranslation()

  return (
    <div className='space-y-2'>
      <FormLabel>{t('Volatile breakpoint tail markers (Claude)')}</FormLabel>
      <FormDescription>
        {t(
          'Substrings that identify a trailing message block rewritten every turn (such as a per-turn token-budget reminder), used by Fix Misplaced Cache Breakpoints and Fallback Cache Breakpoint to avoid placing a breakpoint there. Leave empty to use the built-in default, which only recognizes Claude Code\'s own reminder.'
        )}
      </FormDescription>
      <MultiSelect
        options={defaultPatternOptions}
        selected={props.value}
        allowCreate
        placeholder={t('Built-in default')}
        onChange={props.onChange}
      />
    </div>
  )
}
