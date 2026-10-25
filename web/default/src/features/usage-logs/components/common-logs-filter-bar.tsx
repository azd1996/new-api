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
import { useQueryClient, useIsFetching, useQuery } from '@tanstack/react-query'
import { useNavigate, getRouteApi } from '@tanstack/react-router'
import type { Table } from '@tanstack/react-table'
import { Eye, EyeOff } from 'lucide-react'
import { useState, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import type { ComboboxInputOption } from '@/components/ui/combobox-input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getEnabledModels } from '@/features/channels/api'
import { getCustomers } from '@/features/customers/api'
import { getGroups } from '@/features/users/api'

import { startSessionLogSearch } from '../api'
import {
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
  LOG_TYPE_FILTERS,
} from '../constants'
import { buildSearchParams } from '../lib/filter'
import { getDefaultTimeRange } from '../lib/utils'
import type { CommonLogFilters, LogFilterOperator } from '../types'
import { CommonLogsStats } from './common-logs-stats'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from './logs-filter-toolbar'
import { useLogsViewScope, useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

// Error-keyword filter only applies to error logs (backend LIKE-matches the
// content column gated on type=error), so the input is shown only for this type.
const LOG_TYPE_ERROR_VALUE = String(LOG_TYPE_ENUM.ERROR)

// Sentinel sent for the "其他 / Other (no customer info)" dropdown choice; the
// backend (model.applyTraceTagFilters) matches logs that carry no decoded
// customer at all.
const CUSTOMER_NONE_VALUE = '__none__'

type LogTypeValue = (typeof LOG_TYPE_FILTERS)[number]['value']
const logTypeValueSet = new Set<string>(
  LOG_TYPE_FILTERS.map((type) => type.value)
)

type CommonLogDraft = {
  sourceKey: string
  filters: CommonLogFilters
  logType: LogTypeValue
}

function getFilterValues(value: string | undefined): string[] {
  return value
    ? value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
    : []
}

function serializeFilterValues(values: string[]): string | undefined {
  const normalized = values.map((value) => value.trim()).filter(Boolean)
  return normalized.length > 0 ? normalized.join(',') : undefined
}

function isLogTypeValue(value: string): value is LogTypeValue {
  return logTypeValueSet.has(value)
}

function getLogTypeValue(value: unknown): LogTypeValue {
  return Array.isArray(value) &&
    value.length === 1 &&
    typeof value[0] === 'string' &&
    isLogTypeValue(value[0])
    ? value[0]
    : LOG_TYPE_ALL_VALUE
}

function LogFilterModeButton(props: {
  fieldLabel: string
  operator: LogFilterOperator | undefined
  onChange: (operator: LogFilterOperator) => void
}) {
  const { t } = useTranslation()
  const isExclude = props.operator === 'exclude'
  const nextOperator: LogFilterOperator = isExclude ? 'include' : 'exclude'
  const modeLabel = isExclude ? t('Exclude') : t('Equals')
  return (
    <Button
      type='button'
      variant='outline'
      size='icon'
      className='size-8 shrink-0 font-mono text-sm'
      onClick={() => props.onChange(nextOperator)}
      aria-label={`${props.fieldLabel}: ${modeLabel}`}
      title={`${props.fieldLabel}: ${modeLabel}`}
      aria-pressed={isExclude}
    >
      {isExclude ? '≠' : '='}
    </Button>
  )
}

function buildSearchSourceKey(values: {
  startTime?: unknown
  endTime?: unknown
  channel?: unknown
  channelOp?: unknown
  model?: unknown
  modelOp?: unknown
  token?: unknown
  tokenOp?: unknown
  group?: unknown
  groupOp?: unknown
  username?: unknown
  usernameOp?: unknown
  requestId?: unknown
  upstreamRequestId?: unknown
  statusCode?: unknown
  retryTriggered?: unknown
  errorKeyword?: unknown
  errorSignatureHash?: unknown
  sessionId?: unknown
  customerName?: unknown
  customerId?: unknown
  downstreamRequestId?: unknown
  downstreamTraceTag?: unknown
  type?: unknown
}) {
  return [
    values.startTime,
    values.endTime,
    values.channel,
    values.channelOp,
    values.model,
    values.modelOp,
    values.token,
    values.tokenOp,
    values.group,
    values.groupOp,
    values.username,
    values.usernameOp,
    values.requestId,
    values.upstreamRequestId,
    values.statusCode,
    values.retryTriggered,
    values.errorKeyword,
    values.errorSignatureHash,
    values.sessionId,
    values.customerName,
    values.customerId,
    values.downstreamRequestId,
    values.downstreamTraceTag,
    Array.isArray(values.type) ? values.type.join(',') : values.type,
  ]
    .map((value) => String(value ?? ''))
    .join('\u001f')
}

interface CommonLogsFilterBarProps<TData> {
  table: Table<TData>
  showStats?: boolean
}

export function CommonLogsFilterBar<TData>(
  props: CommonLogsFilterBarProps<TData>
) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const searchParams = route.useSearch()
  const { isAdminView: isAdmin, canManageScope } = useLogsViewScope()
  const { sensitiveVisible, setSensitiveVisible } = useUsageLogsContext()
  const fetchingLogs = useIsFetching({ queryKey: ['logs'] })

  // Site-wide candidates for the model/group filters. Models come from enabled
  // channel abilities (only models actually configured/usable), not the full
  // built-in catalog. Both source endpoints are admin-only, so the queries only
  // run for users who can manage scope; everyone else still gets a free-text
  // combobox (manual input is always allowed).
  const modelCandidatesQuery = useQuery({
    queryKey: ['log-filter-models'],
    queryFn: getEnabledModels,
    enabled: canManageScope,
    staleTime: 5 * 60 * 1000,
  })
  const groupCandidatesQuery = useQuery({
    queryKey: ['log-filter-groups'],
    queryFn: getGroups,
    enabled: canManageScope,
    staleTime: 5 * 60 * 1000,
  })
  // Configured customers back the customer name/id dropdowns (admin-only).
  const customerCandidatesQuery = useQuery({
    queryKey: ['log-filter-customers'],
    queryFn: () => getCustomers({ p: 1, page_size: 1000 }),
    enabled: canManageScope,
    staleTime: 5 * 60 * 1000,
  })
  const modelOptions = useMemo(
    () =>
      (modelCandidatesQuery.data?.data ?? []).map((model) => ({
        value: model,
        label: model,
      })),
    [modelCandidatesQuery.data]
  )
  const groupOptions = useMemo(
    () =>
      (groupCandidatesQuery.data?.data ?? []).map((group) => ({
        value: group,
        label: group,
      })),
    [groupCandidatesQuery.data]
  )
  const noneOption = useMemo<ComboboxInputOption>(
    () => ({
      value: CUSTOMER_NONE_VALUE,
      label: t('Other (no customer info)'),
    }),
    [t]
  )
  const customerNameOptions = useMemo<ComboboxInputOption[]>(() => {
    const names = [
      ...new Set(
        (customerCandidatesQuery.data?.data?.items ?? [])
          .map((customer) => customer.name)
          .filter((name): name is string => !!name)
      ),
    ].map((name) => ({ value: name, label: name }))
    return [...names, noneOption]
  }, [customerCandidatesQuery.data, noneOption])
  const customerIdOptions = useMemo<ComboboxInputOption[]>(() => {
    const ids = (customerCandidatesQuery.data?.data?.items ?? []).map(
      (customer) => ({
        value: customer.customer_id,
        label: customer.name
          ? `${customer.customer_id} (${customer.name})`
          : customer.customer_id,
      })
    )
    return [...ids, noneOption]
  }, [customerCandidatesQuery.data, noneOption])

  const searchState = useMemo<CommonLogDraft>(() => {
    const { start, end } = getDefaultTimeRange()
    const sourceValues = {
      startTime: searchParams.startTime,
      endTime: searchParams.endTime,
      channel: searchParams.channel,
      channelOp: searchParams.channelOp,
      model: searchParams.model,
      modelOp: searchParams.modelOp,
      token: searchParams.token,
      tokenOp: searchParams.tokenOp,
      group: searchParams.group,
      groupOp: searchParams.groupOp,
      username: searchParams.username,
      usernameOp: searchParams.usernameOp,
      requestId: searchParams.requestId,
      upstreamRequestId: searchParams.upstreamRequestId,
      statusCode: searchParams.statusCode,
      retryTriggered: isAdmin ? searchParams.retryTriggered : undefined,
      errorKeyword: searchParams.errorKeyword,
      errorSignatureHash: searchParams.errorSignatureHash,
      sessionId: searchParams.sessionId,
      customerName: searchParams.customerName,
      customerId: searchParams.customerId,
      downstreamRequestId: searchParams.downstreamRequestId,
      downstreamTraceTag: searchParams.downstreamTraceTag,
      type: searchParams.type,
    }
    const filters: CommonLogFilters = {
      startTime: searchParams.startTime
        ? new Date(searchParams.startTime)
        : start,
      endTime: searchParams.endTime ? new Date(searchParams.endTime) : end,
      channel: searchParams.channel || undefined,
      channelOp: searchParams.channelOp || 'include',
      model: searchParams.model || undefined,
      modelOp: searchParams.modelOp || 'include',
      token: searchParams.token || undefined,
      tokenOp: searchParams.tokenOp || 'include',
      group: searchParams.group || undefined,
      groupOp: searchParams.groupOp || 'include',
      username: searchParams.username || undefined,
      usernameOp: searchParams.usernameOp || 'include',
      requestId: searchParams.requestId || undefined,
      upstreamRequestId: searchParams.upstreamRequestId || undefined,
      statusCode: searchParams.statusCode || undefined,
      retryTriggered: isAdmin ? searchParams.retryTriggered : undefined,
      errorKeyword: searchParams.errorKeyword || undefined,
      errorSignatureHash: searchParams.errorSignatureHash || undefined,
      errorSignature: searchParams.errorSignature || undefined,
      sessionId: searchParams.sessionId || undefined,
      customerName: searchParams.customerName || undefined,
      customerId: searchParams.customerId || undefined,
      downstreamRequestId: searchParams.downstreamRequestId || undefined,
      downstreamTraceTag: searchParams.downstreamTraceTag || undefined,
    }
    return {
      sourceKey: buildSearchSourceKey(sourceValues),
      filters,
      logType: getLogTypeValue(searchParams.type),
    }
  }, [
    isAdmin,
    searchParams.startTime,
    searchParams.endTime,
    searchParams.channel,
    searchParams.channelOp,
    searchParams.model,
    searchParams.modelOp,
    searchParams.token,
    searchParams.tokenOp,
    searchParams.group,
    searchParams.groupOp,
    searchParams.username,
    searchParams.usernameOp,
    searchParams.requestId,
    searchParams.upstreamRequestId,
    searchParams.statusCode,
    searchParams.retryTriggered,
    searchParams.errorKeyword,
    searchParams.errorSignatureHash,
    searchParams.errorSignature,
    searchParams.sessionId,
    searchParams.customerName,
    searchParams.customerId,
    searchParams.downstreamRequestId,
    searchParams.downstreamTraceTag,
    searchParams.type,
  ])
  const [draft, setDraft] = useState<CommonLogDraft>(() => searchState)
  const activeDraft =
    draft.sourceKey === searchState.sourceKey ? draft : searchState
  const filters = activeDraft.filters
  const logType = activeDraft.logType

  const handleChange = useCallback(
    (
      field: keyof CommonLogFilters,
      value: Date | string | LogFilterOperator | undefined
    ) => {
      setDraft((current) => {
        const base =
          current.sourceKey === searchState.sourceKey ? current : searchState
        return {
          sourceKey: searchState.sourceKey,
          filters: { ...base.filters, [field]: value },
          logType: base.logType,
        }
      })
    },
    [searchState]
  )

  // Session search needs an explicit username + time range; "notfound" finds
  // logs with no session id. Returns the query, or null (after a toast) if the
  // required fields are missing.
  const buildSessionQuery = useCallback(() => {
    const sessionId = (filters.sessionId || '').trim()
    if (!sessionId) return null
    const username = (filters.username || '').trim()
    const startSec = filters.startTime
      ? Math.floor(filters.startTime.getTime() / 1000)
      : 0
    const endSec = filters.endTime
      ? Math.floor(filters.endTime.getTime() / 1000)
      : 0
    if (!username || !startSec || !endSec) {
      toast.error(t('Username and time range are required for session search'))
      return null
    }
    return {
      session_id: sessionId,
      username,
      start_timestamp: startSec,
      end_timestamp: endSec,
    }
  }, [filters, t])

  const applyFilters = useCallback(() => {
    const filterParams = buildSearchParams(filters, 'common')
    navigate({
      to: '/usage-logs/$section',
      params: { section: 'common' },
      search: {
        ...filterParams,
        type: [logType],
        page: 1,
      },
    })
    queryClient.invalidateQueries({ queryKey: ['logs'] })
    queryClient.invalidateQueries({ queryKey: ['usage-logs-stats'] })
    queryClient.invalidateQueries({ queryKey: ['session-logs'] })
  }, [filters, logType, navigate, queryClient])

  const handleApply = useCallback(() => {
    if ((filters.sessionId || '').trim() && !buildSessionQuery()) return
    applyFilters()
  }, [filters.sessionId, buildSessionQuery, applyFilters])

  // Force Search: for a session-id query, back up any cached result and rescan;
  // identical to Search when no session id is entered.
  const handleForceApply = useCallback(async () => {
    const query = buildSessionQuery()
    if (!query) {
      if (!(filters.sessionId || '').trim()) applyFilters()
      return
    }
    try {
      await startSessionLogSearch(query, true)
    } catch {
      toast.error(t('Failed to start session search'))
      return
    }
    applyFilters()
  }, [buildSessionQuery, applyFilters, filters.sessionId, t])

  const handleReset = useCallback(() => {
    const { start, end } = getDefaultTimeRange()
    const resetFilters: CommonLogFilters = { startTime: start, endTime: end }
    const resetSearch = {
      type: [LOG_TYPE_ALL_VALUE],
      startTime: start.getTime(),
      endTime: end.getTime(),
    }
    setDraft({
      sourceKey: buildSearchSourceKey(resetSearch),
      filters: resetFilters,
      logType: LOG_TYPE_ALL_VALUE,
    })

    navigate({
      to: '/usage-logs/$section',
      params: { section: 'common' },
      search: {
        page: 1,
        ...resetSearch,
      },
    })
    queryClient.invalidateQueries({ queryKey: ['logs'] })
    queryClient.invalidateQueries({ queryKey: ['usage-logs-stats'] })
  }, [navigate, queryClient])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleApply()
    },
    [handleApply]
  )

  const hasExpandedFilters =
    !!filters.token ||
    !!filters.username ||
    !!filters.channel ||
    !!filters.requestId ||
    !!filters.upstreamRequestId ||
    !!filters.statusCode ||
    !!filters.retryTriggered ||
    !!filters.errorKeyword ||
    !!filters.errorSignatureHash ||
    !!filters.sessionId ||
    !!filters.customerName ||
    !!filters.customerId ||
    !!filters.downstreamRequestId ||
    !!filters.downstreamTraceTag

  const hasTypeFilter = logType !== LOG_TYPE_ALL_VALUE
  const hasAdditionalFilters =
    !!filters.model || !!filters.group || hasTypeFilter || hasExpandedFilters

  const isErrorLogType = logType === LOG_TYPE_ERROR_VALUE
  const expandedFilterCount = [
    filters.token,
    isAdmin ? filters.username : undefined,
    isAdmin ? filters.channel : undefined,
    filters.requestId,
    filters.upstreamRequestId,
    filters.statusCode,
    isAdmin ? filters.retryTriggered : undefined,
    isErrorLogType ? filters.errorKeyword : undefined,
    isAdmin ? filters.errorSignatureHash : undefined,
    isAdmin ? filters.sessionId : undefined,
    isAdmin ? filters.customerName : undefined,
    isAdmin ? filters.customerId : undefined,
    isAdmin ? filters.downstreamRequestId : undefined,
    isAdmin ? filters.downstreamTraceTag : undefined,
  ].filter(Boolean).length
  const logTypeItems = useMemo(
    () =>
      LOG_TYPE_FILTERS.map((type) => ({
        value: type.value,
        label: t(type.label),
      })),
    [t]
  )
  const logTypeLabel =
    logTypeItems.find((type) => type.value === logType)?.label ?? t('All Types')
  const retryTriggeredItems = [
    { value: 'all', label: t('All') },
    { value: 'true', label: t('Yes') },
    { value: 'false', label: t('No') },
  ]
  const retryTriggeredLabel =
    retryTriggeredItems.find(
      (item) => item.value === (filters.retryTriggered ?? 'all')
    )?.label ?? t('All')

  const statsBar = (
    <div className='flex flex-wrap items-center gap-2'>
      <CommonLogsStats />
    </div>
  )
  const sensitiveToggle = (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='ghost'
            size='icon'
            onClick={() => setSensitiveVisible(!sensitiveVisible)}
            aria-label={sensitiveVisible ? t('Hide') : t('Show')}
            className='text-muted-foreground hover:text-foreground size-7'
          />
        }
      >
        {sensitiveVisible ? <Eye /> : <EyeOff />}
      </TooltipTrigger>
      <TooltipContent>
        {sensitiveVisible ? t('Hide') : t('Show')}
      </TooltipContent>
    </Tooltip>
  )

  const dateRangeFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={filters.startTime}
        end={filters.endTime}
        onChange={({ start, end }) => {
          handleChange('startTime', start)
          handleChange('endTime', end)
        }}
      />
    </LogsFilterField>
  )
  const modelFilter = (
    <LogsFilterField>
      <div className='flex min-w-0 gap-1'>
        <MultiSelect
          options={modelOptions}
          selected={getFilterValues(filters.model)}
          onChange={(values) =>
            handleChange('model', serializeFilterValues(values))
          }
          placeholder={t('Model Name')}
          emptyText={t('No option found.')}
          allowCreate
          className='max-h-16 min-h-8 min-w-0 flex-1 overflow-y-auto text-sm leading-5'
        />
        <LogFilterModeButton
          fieldLabel={t('Model Name')}
          operator={filters.modelOp}
          onChange={(value) => handleChange('modelOp', value)}
        />
      </div>
    </LogsFilterField>
  )
  const groupFilter = (
    <LogsFilterField>
      <div className='flex min-w-0 gap-1'>
        <MultiSelect
          options={groupOptions}
          selected={getFilterValues(filters.group)}
          onChange={(values) =>
            handleChange('group', serializeFilterValues(values))
          }
          placeholder={t('Group')}
          emptyText={t('No option found.')}
          allowCreate
          className='max-h-16 min-h-8 min-w-0 flex-1 overflow-y-auto text-sm leading-5'
        />
        <LogFilterModeButton
          fieldLabel={t('Group')}
          operator={filters.groupOp}
          onChange={(value) => handleChange('groupOp', value)}
        />
      </div>
    </LogsFilterField>
  )
  const typeFilter = (
    <LogsFilterField>
      <Select
        items={logTypeItems}
        value={logType}
        onValueChange={(value) => {
          const nextLogType =
            value !== null && isLogTypeValue(value) ? value : LOG_TYPE_ALL_VALUE
          setDraft((current) => {
            const base =
              current.sourceKey === searchState.sourceKey
                ? current
                : searchState
            // The error-keyword filter only applies to error logs; drop it when
            // switching to any other type so it is not sent with the next query.
            const nextFilters =
              nextLogType === LOG_TYPE_ERROR_VALUE
                ? base.filters
                : { ...base.filters, errorKeyword: undefined }
            return {
              sourceKey: searchState.sourceKey,
              filters: nextFilters,
              logType: nextLogType,
            }
          })
        }}
      >
        <SelectTrigger>
          <SelectValue>{logTypeLabel}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {LOG_TYPE_FILTERS.map((type) => (
              <SelectItem key={type.value} value={type.value}>
                {t(type.label)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </LogsFilterField>
  )
  const advancedFilters = (
    <>
      {isAdmin && (
        <LogsFilterField>
          <Select
            items={retryTriggeredItems}
            value={filters.retryTriggered ?? 'all'}
            onValueChange={(value) =>
              handleChange(
                'retryTriggered',
                value === 'true' || value === 'false' ? value : undefined
              )
            }
          >
            <SelectTrigger aria-label={t('Retry triggered')}>
              <SelectValue>
                {t('Retry triggered')}: {retryTriggeredLabel}
              </SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {retryTriggeredItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </LogsFilterField>
      )}
      <LogsFilterField>
        <div className='flex min-w-0 gap-1'>
          <MultiSelect
            options={[]}
            selected={getFilterValues(filters.token)}
            onChange={(values) =>
              handleChange('token', serializeFilterValues(values))
            }
            placeholder={t('Token Name')}
            allowCreate
            renderSelectedSummary={
              sensitiveVisible
                ? undefined
                : (values) => '•'.repeat(Math.min(values.length * 4, 12))
            }
            className='max-h-16 min-h-8 min-w-0 flex-1 overflow-y-auto text-sm leading-5'
          />
          <LogFilterModeButton
            fieldLabel={t('Token Name')}
            operator={filters.tokenOp}
            onChange={(value) => handleChange('tokenOp', value)}
          />
        </div>
      </LogsFilterField>
      {isAdmin && (
        <LogsFilterField>
          <div className='flex min-w-0 gap-1'>
            <MultiSelect
              options={[]}
              selected={getFilterValues(filters.username)}
              onChange={(values) =>
                handleChange('username', serializeFilterValues(values))
              }
              placeholder={t('Username')}
              allowCreate
              renderSelectedSummary={
                sensitiveVisible
                  ? undefined
                  : (values) => '•'.repeat(Math.min(values.length * 4, 12))
              }
              className='max-h-16 min-h-8 min-w-0 flex-1 overflow-y-auto text-sm leading-5'
            />
            <LogFilterModeButton
              fieldLabel={t('Username')}
              operator={filters.usernameOp}
              onChange={(value) => handleChange('usernameOp', value)}
            />
          </div>
        </LogsFilterField>
      )}
      {isAdmin && (
        <LogsFilterField>
          <div className='flex min-w-0 gap-1'>
            <MultiSelect
              options={[]}
              selected={getFilterValues(filters.channel)}
              onChange={(values) =>
                handleChange('channel', serializeFilterValues(values))
              }
              placeholder={t('Channel ID')}
              allowCreate
              className='max-h-16 min-h-8 min-w-0 flex-1 overflow-y-auto text-sm leading-5'
            />
            <LogFilterModeButton
              fieldLabel={t('Channel ID')}
              operator={filters.channelOp}
              onChange={(value) => handleChange('channelOp', value)}
            />
          </div>
        </LogsFilterField>
      )}
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Request ID')}
          value={filters.requestId || ''}
          onChange={(e) => handleChange('requestId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Upstream Request ID')}
          value={filters.upstreamRequestId || ''}
          onChange={(e) => handleChange('upstreamRequestId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Status Code')}
          inputMode='numeric'
          value={filters.statusCode || ''}
          onChange={(e) =>
            handleChange('statusCode', e.target.value.replaceAll(/\D/g, ''))
          }
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      {isAdmin && (
        <LogsFilterField>
          <LogsFilterInput
            placeholder={t('Session ID')}
            value={filters.sessionId || ''}
            onChange={(e) => handleChange('sessionId', e.target.value)}
            onKeyDown={handleKeyDown}
          />
        </LogsFilterField>
      )}
      {isAdmin && !!(filters.sessionId || '').trim() && (
        <LogsFilterField>
          <Button
            variant='outline'
            onClick={handleForceApply}
            className='h-8 w-full'
          >
            {t('Force Search')}
          </Button>
        </LogsFilterField>
      )}
      {isAdmin && (
        <div className='col-span-full grid grid-cols-1 gap-2 sm:grid-cols-[repeat(auto-fit,minmax(10rem,1fr))]'>
          <LogsFilterField>
            <Combobox
              options={customerNameOptions}
              value={filters.customerName || ''}
              onValueChange={(value) =>
                handleChange('customerName', value || undefined)
              }
              placeholder={t('Customer Name')}
              searchPlaceholder={t('Customer Name')}
              emptyText={t('No option found.')}
              allowCustomValue
              clearable
              className='h-8 min-w-0 text-sm leading-5'
            />
          </LogsFilterField>
          <LogsFilterField>
            <Combobox
              options={customerIdOptions}
              value={filters.customerId || ''}
              onValueChange={(value) =>
                handleChange('customerId', value || undefined)
              }
              placeholder={t('Customer ID')}
              searchPlaceholder={t('Customer ID')}
              emptyText={t('No option found.')}
              allowCustomValue
              clearable
              className='h-8 min-w-0 text-sm leading-5'
            />
          </LogsFilterField>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Downstream Request ID')}
              value={filters.downstreamRequestId || ''}
              onChange={(e) =>
                handleChange('downstreamRequestId', e.target.value)
              }
              onKeyDown={handleKeyDown}
            />
          </LogsFilterField>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Downstream Trace Tag')}
              value={filters.downstreamTraceTag || ''}
              onChange={(e) =>
                handleChange('downstreamTraceTag', e.target.value)
              }
              onKeyDown={handleKeyDown}
            />
          </LogsFilterField>
        </div>
      )}
      {isAdmin && filters.errorSignatureHash && (
        <div className='col-span-full grid grid-cols-1 gap-2 sm:grid-cols-[repeat(auto-fit,minmax(10rem,1fr))]'>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Error Signature')}
              value={filters.errorSignature || filters.errorSignatureHash}
              title={filters.errorSignature || filters.errorSignatureHash}
              readOnly
              className='cursor-default'
            />
          </LogsFilterField>
        </div>
      )}
      {isErrorLogType && (
        <div className='col-span-full grid grid-cols-1 gap-2 sm:grid-cols-[repeat(auto-fit,minmax(10rem,1fr))]'>
          <LogsFilterField>
            <LogsFilterInput
              placeholder={t('Error Keyword')}
              value={filters.errorKeyword || ''}
              onChange={(e) => handleChange('errorKeyword', e.target.value)}
              onKeyDown={handleKeyDown}
            />
          </LogsFilterField>
        </div>
      )}
    </>
  )

  return (
    <LogsFilterToolbar
      table={props.table}
      stats={props.showStats === false ? undefined : statsBar}
      actionStart={sensitiveToggle}
      primaryFilters={
        <div className='flex min-w-0 flex-1 flex-col gap-2 sm:flex-row'>
          <div className='min-w-0 flex-[7]'>{dateRangeFilter}</div>
          <div className='min-w-0 flex-[6]'>{modelFilter}</div>
          <div className='min-w-0 flex-[8]'>{groupFilter}</div>
          <div className='min-w-0 flex-[3]'>{typeFilter}</div>
        </div>
      }
      advancedFilters={advancedFilters}
      mobilePinnedFilters={dateRangeFilter}
      mobileFilters={
        <>
          {modelFilter}
          {groupFilter}
          {typeFilter}
          {advancedFilters}
        </>
      }
      mobileFilterCount={
        [filters.model, filters.group, hasTypeFilter].filter(Boolean).length +
        expandedFilterCount
      }
      hasAdvancedActiveFilters={hasExpandedFilters}
      advancedFilterCount={expandedFilterCount}
      hasActiveFilters={hasAdditionalFilters}
      onSearch={handleApply}
      searchLoading={fetchingLogs > 0}
      onReset={handleReset}
    />
  )
}
