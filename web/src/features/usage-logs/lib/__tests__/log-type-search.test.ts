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

import { LOG_TYPE_ENUM, LOG_TYPE_FILTERS } from '../../constants'
import { logTypeSearchSchema } from '../log-type-search'

describe('usage log type search param', () => {
  test.each(LOG_TYPE_FILTERS.map((type) => type.value))(
    'keeps log type %s selected after validation',
    (value) => {
      expect(logTypeSearchSchema.parse([value])).toEqual([value])
    }
  )

  test('keeps the retry-override type instead of falling back to all types', () => {
    const value = String(LOG_TYPE_ENUM.RETRY_OVERRIDE)
    expect(logTypeSearchSchema.parse(value)).toEqual([value])
  })

  test('falls back to all types for an unknown log type', () => {
    expect(logTypeSearchSchema.parse(['99'])).toEqual([])
  })
})
