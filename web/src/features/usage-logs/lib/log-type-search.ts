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
import z from 'zod'

import { LOG_TYPE_FILTERS } from '../constants'

// Accepted values come from LOG_TYPE_FILTERS so a newly added log type stays
// selectable: an unknown value fails validation and the whole param falls
// back to "all types".
const logTypeValueSet = new Set<string>(
  LOG_TYPE_FILTERS.map((type) => type.value)
)

export const logTypeSearchSchema = z
  .preprocess(
    (value) => {
      if (value == null || value === '') return undefined
      return Array.isArray(value) ? value : [value]
    },
    z.array(z.string().refine((value) => logTypeValueSet.has(value))).optional()
  )
  .catch([])
