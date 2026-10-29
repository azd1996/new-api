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

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { MultiSelect } from '@/components/multi-select'
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
import { getGroups } from '@/features/users/api'
import { useMediaQuery } from '@/hooks'
import { getUserGroups } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import { LOG_TYPE_ALL_VALUE, LOG_TYPE_FILTERS } from '../constants'
import { buildSearchParams } from '../lib/filter'
import { getDefaultTimeRange } from '../lib/utils'
import type { CommonLogFilters, LogFilterOperator } from '../types'
import { CommonLogsStats } from './common-logs-stats'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import { ExportLogsDialog } from './dialogs/export-logs-dialog'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from './logs-filter-toolbar'
import { useLogsViewScope, useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

type LogTypeValue = (typeof LOG_TYPE_FILTERS)[number]['value']
const logTypeValueSet = new Set<string>(
  LOG_TYPE_FILTERS.map((type) => type.value)
)

type CommonLogDraft = {
  sourceKey: string
  filters: CommonLogFilters
  logType: LogTypeValue
}

function isLogTypeValue(value: string): value is LogTypeValue {
  return logTypeValueSet.has(value)
}

// A multi-value filter is carried as a comma-separated string in the search
// params. These two helpers convert between that wire form and the MultiSelect
// chip array.
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

// LogFilterModeButton toggles a field between include (=) and exclude (≠).
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

function getLogTypeValue(value: unknown): LogTypeValue {
  return Array.isArray(value) &&
    value.length === 1 &&
    typeof value[0] === 'string' &&
    isLogTypeValue(value[0])
    ? value[0]
    : LOG_TYPE_ALL_VALUE
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
  sessionId?: unknown
  downstreamRequestId?: unknown
  statusCode?: unknown
  errorKeyword?: unknown
  errorSignatureHash?: unknown
  errorSignature?: unknown
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
    values.sessionId,
    values.downstreamRequestId,
    values.statusCode,
    values.errorKeyword,
    values.errorSignatureHash,
    values.errorSignature,
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
  const isMobile = useMediaQuery('(max-width: 640px)')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const searchParams = route.useSearch()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const { sensitiveVisible, setSensitiveVisible } = useUsageLogsContext()
  const fetchingLogs = useIsFetching({ queryKey: ['logs'] })
  const { data: adminGroups } = useQuery({
    queryKey: ['groups'],
    queryFn: async () => requireServerSuccess(await getGroups()),
    enabled: isAdmin,
  })
  const { data: userGroups } = useQuery({
    queryKey: ['user-groups'],
    queryFn: async () => requireServerSuccess(await getUserGroups()),
    enabled: !isAdmin,
  })
  const groupOptions = useMemo(() => {
    const groups = isAdmin
      ? (adminGroups?.data ?? [])
      : Object.keys(userGroups?.data ?? {})
    return groups
      .filter((group) => group !== 'auto')
      .map((group) => ({ label: group, value: group }))
  }, [isAdmin, adminGroups, userGroups])

  // Site-wide model candidates sourced from enabled channel abilities (only
  // models actually configured/usable, not the full built-in catalog). The
  // endpoint is admin-only, so the query only runs for the admin view; everyone
  // else still gets a free-text MultiSelect via allowCreate.
  const { data: enabledModels } = useQuery({
    queryKey: ['log-filter-models'],
    queryFn: async () => requireServerSuccess(await getEnabledModels()),
    enabled: isAdmin,
    staleTime: 5 * 60 * 1000,
  })
  const modelOptions = useMemo(
    () => (enabledModels?.data ?? []).map((model) => ({ label: model, value: model })),
    [enabledModels]
  )

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
      sessionId: searchParams.sessionId,
      downstreamRequestId: searchParams.downstreamRequestId,
      statusCode: searchParams.statusCode,
      errorKeyword: searchParams.errorKeyword,
      errorSignatureHash: searchParams.errorSignatureHash,
      errorSignature: searchParams.errorSignature,
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
      sessionId: searchParams.sessionId || undefined,
      downstreamRequestId: searchParams.downstreamRequestId || undefined,
      statusCode: searchParams.statusCode || undefined,
      errorKeyword: searchParams.errorKeyword || undefined,
      errorSignatureHash: searchParams.errorSignatureHash || undefined,
      errorSignature: searchParams.errorSignature || undefined,
    }
    return {
      sourceKey: buildSearchSourceKey(sourceValues),
      filters,
      logType: getLogTypeValue(searchParams.type),
    }
  }, [
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
    searchParams.sessionId,
    searchParams.downstreamRequestId,
    searchParams.statusCode,
    searchParams.errorKeyword,
    searchParams.errorSignatureHash,
    searchParams.errorSignature,
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

  const handleApply = useCallback(
    (nextFilters: CommonLogFilters = filters) => {
      const filterParams = buildSearchParams(nextFilters, 'common')
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
    },
    [filters, logType, navigate, queryClient]
  )

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
    !!filters.sessionId ||
    !!filters.downstreamRequestId ||
    !!filters.statusCode ||
    !!filters.errorKeyword ||
    !!filters.errorSignatureHash

  const hasTypeFilter = logType !== LOG_TYPE_ALL_VALUE
  const hasAdditionalFilters =
    !!filters.model || !!filters.group || hasTypeFilter || hasExpandedFilters

  const expandedFilterCount = [
    filters.token,
    isAdmin ? filters.username : undefined,
    isAdmin ? filters.channel : undefined,
    filters.requestId,
    filters.upstreamRequestId,
    filters.sessionId,
    isAdmin ? filters.downstreamRequestId : undefined,
    filters.statusCode,
    filters.errorKeyword,
    isAdmin ? filters.errorSignatureHash : undefined,
  ].filter(Boolean).length
  const logTypeItems = useMemo(
    () =>
      LOG_TYPE_FILTERS.map((type) => ({
        value: type.value,
        label: t(type.label),
        deprecated: type.deprecated,
      })),
    [t]
  )
  const selectedLogType = logTypeItems.find((type) => type.value === logType)
  const deprecatedTypeDescription = t(
    'Only used to find historical logs. New records are available in Audit Logs.'
  )

  const statsBar = <CommonLogsStats />
  const sensitiveToggle = (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant='ghost'
            size='icon'
            onClick={() => setSensitiveVisible(!sensitiveVisible)}
            aria-label={sensitiveVisible ? t('Hide') : t('Show')}
            className='text-muted-foreground hover:text-foreground size-7 max-sm:size-11'
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
          if (isMobile) {
            handleApply({ ...filters, startTime: start, endTime: end })
          }
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
          emptyText={t('No group found.')}
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
            return {
              sourceKey: searchState.sourceKey,
              filters: base.filters,
              logType: nextLogType,
            }
          })
        }}
      >
        <SelectTrigger
          aria-label={t('Type')}
          aria-description={
            selectedLogType?.deprecated ? deprecatedTypeDescription : undefined
          }
        >
          <SelectValue className='min-w-0'>
            <span className='truncate'>
              {selectedLogType?.label ?? t('All Types')}
            </span>
            {selectedLogType?.deprecated && (
              <Badge
                variant='secondary'
                className='h-4 px-1.5 text-[10px] font-normal'
                title={deprecatedTypeDescription}
              >
                {t('Deprecated')}
              </Badge>
            )}
          </SelectValue>
        </SelectTrigger>
        <SelectContent
          alignItemWithTrigger={false}
          className='max-w-[calc(100vw-2rem)] min-w-52'
        >
          <SelectGroup>
            {LOG_TYPE_FILTERS.map((type) => (
              <SelectItem
                key={type.value}
                value={type.value}
                className='[&_[data-slot=select-item-text]]:items-center'
                aria-description={
                  type.deprecated ? deprecatedTypeDescription : undefined
                }
              >
                {t(type.label)}
                {type.deprecated && (
                  <Badge
                    variant='secondary'
                    className='h-4 px-1.5 text-[10px] font-normal'
                    title={deprecatedTypeDescription}
                  >
                    {t('Deprecated')}
                  </Badge>
                )}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </LogsFilterField>
  )
  const advancedFilters = (
    <>
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
          placeholder={t('Session ID')}
          value={filters.sessionId || ''}
          onChange={(e) => handleChange('sessionId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      {isAdmin && (
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
      )}
      <LogsFilterField>
        <LogsFilterInput
          type='number'
          placeholder={t('Status Code')}
          value={filters.statusCode || ''}
          onChange={(e) => handleChange('statusCode', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Error Keyword')}
          value={filters.errorKeyword || ''}
          onChange={(e) => handleChange('errorKeyword', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      {isAdmin && filters.errorSignatureHash && (
        <LogsFilterField>
          <LogsFilterInput
            readOnly
            placeholder={t('Error Signature')}
            value={filters.errorSignature || filters.errorSignatureHash}
          />
        </LogsFilterField>
      )}
    </>
  )

  return (
    <LogsFilterToolbar
      table={props.table}
      compactMobile
      stats={props.showStats === false ? undefined : statsBar}
      actionStart={sensitiveToggle}
      actionEnd={<ExportLogsDialog />}
      primaryFilters={
        <>
          {dateRangeFilter}
          {modelFilter}
          {groupFilter}
          {typeFilter}
        </>
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
      onSearch={() => handleApply()}
      searchLoading={fetchingLogs > 0}
      onReset={handleReset}
    />
  )
}
