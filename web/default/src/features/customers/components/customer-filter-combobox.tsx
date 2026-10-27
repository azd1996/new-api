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
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'

import { getCustomers } from '../api'

// Sentinel value both the usage-log and error-stats filters send for the
// "其他 / Other (no customer info)" choice; the backend maps it to logs whose
// Other JSON carries no decoded customer at all.
const CUSTOMER_NONE_VALUE = '__none__'

// useCustomerFilterOptions fetches the admin-configured customers once (shared
// across every combobox instance via the query key) and builds the dropdown
// option lists for the customer-name and customer-id filters, each terminated
// by the "no customer info" sentinel option.
function useCustomerFilterOptions(enabled = true) {
  const { t } = useTranslation()
  const customersQuery = useQuery({
    queryKey: ['filter-customers'],
    queryFn: () => getCustomers({ p: 1, page_size: 1000 }),
    enabled,
    staleTime: 5 * 60 * 1000,
  })
  const noneOption = useMemo<ComboboxInputOption>(
    () => ({ value: CUSTOMER_NONE_VALUE, label: t('Other (no customer info)') }),
    [t]
  )
  const nameOptions = useMemo<ComboboxInputOption[]>(() => {
    const names = [
      ...new Set(
        (customersQuery.data?.data?.items ?? [])
          .map((customer) => customer.name)
          .filter((name): name is string => !!name)
      ),
    ].map((name) => ({ value: name, label: name }))
    return [...names, noneOption]
  }, [customersQuery.data, noneOption])
  const idOptions = useMemo<ComboboxInputOption[]>(() => {
    const ids = (customersQuery.data?.data?.items ?? []).map((customer) => ({
      value: customer.customer_id,
      label: customer.name
        ? `${customer.customer_id} (${customer.name})`
        : customer.customer_id,
    }))
    return [...ids, noneOption]
  }, [customersQuery.data, noneOption])
  return { nameOptions, idOptions }
}

interface CustomerFilterComboboxProps {
  // Which customer field this combobox edits. 'name' matches on the configured
  // display name; 'id' matches on the downstream customer id.
  field: 'name' | 'id'
  value?: string
  onValueChange: (value: string | undefined) => void
  // Gate the underlying customer fetch (admin-only pages pass their admin flag).
  enabled?: boolean
  className?: string
}

// CustomerFilterCombobox renders a dropdown-plus-manual-input filter for a
// customer name or id, backed by the configured customer list. It is shared by
// the usage-log and detailed-error-stats filter bars so both behave identically.
export function CustomerFilterCombobox(props: CustomerFilterComboboxProps) {
  const { t } = useTranslation()
  const { nameOptions, idOptions } = useCustomerFilterOptions(
    props.enabled ?? true
  )
  const isName = props.field === 'name'
  const label = isName ? t('Customer Name') : t('Customer ID')
  return (
    <Combobox
      options={isName ? nameOptions : idOptions}
      value={props.value || ''}
      onValueChange={(value) => props.onValueChange(value || undefined)}
      placeholder={label}
      searchPlaceholder={label}
      emptyText={t('No option found.')}
      allowCustomValue
      clearable
      className={props.className}
    />
  )
}
