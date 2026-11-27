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
import { AlertTriangle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import type { LogOtherData, UsageCheckField } from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

type UsageCheck = NonNullable<
  NonNullable<LogOtherData['admin_info']>['usage_check']
>

// Shows the input usage a Claude stream reported in message_start next to
// what message_delta reported, for the fields that disagree. Field names stay
// in their wire form because operators match them against upstream responses.
export function UsageCheckDetails(props: { check: UsageCheck }) {
  const { t } = useTranslation()
  const tampered = props.check.tampered ?? []
  const unreported = props.check.unreported ?? []
  const comparison = (field: UsageCheckField) =>
    `${field}: ${props.check.start[field] ?? 0} → ${props.check.delta[field] ?? 0}`

  return (
    <DetailSection
      icon={<AlertTriangle className='size-3.5' aria-hidden='true' />}
      label={t('Input usage check failed')}
      variant='danger'
    >
      <p className='mb-1 text-xs wrap-break-word'>
        {t(
          'The stream reported different input usage at its start and at its end. Billing was not changed.'
        )}
      </p>
      {tampered.map((field) => (
        <DetailRow
          key={field}
          label={t('Changed')}
          value={comparison(field)}
          mono
        />
      ))}
      {unreported.map((field) => (
        <DetailRow
          key={field}
          label={t('Not reported')}
          value={comparison(field)}
          mono
        />
      ))}
    </DetailSection>
  )
}
