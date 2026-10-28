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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, useNavigate, useRouter } from '@tanstack/react-router'
import { Lightbulb, SlidersHorizontal } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useIsAdmin } from '@/hooks/use-admin'

import {
  deleteErrorAlias,
  getErrorAliases,
  getErrorStats,
  upsertErrorAlias,
} from './api'
import { ErrorSignatureRuleSuggestionsDialog } from './components/error-signature-rule-suggestions-dialog'
import { ErrorSignatureRulesDialog } from './components/error-signature-rules-dialog'
import {
  ErrorStatsFilterBar,
  type ErrorStatsFilterDraft,
} from './components/error-stats-filter-bar'
import {
  type AliasSaveInput,
  ErrorStatsTable,
} from './components/error-stats-table'
import type {
  ErrorStatItem,
  ErrorStatsGroupBy,
  ErrorStatsParams,
  SignatureRule,
} from './types'

const route = getRouteApi('/_authenticated/error-stats/')

const DAY_IN_MS = 24 * 60 * 60 * 1000
const DEFAULT_LIMIT = 200

// Seed suggestions for the attribution combobox. These are raw data values
// (stored verbatim on the alias) and must NOT be run through t().
const DEFAULT_ATTRIBUTIONS = [
  '客户端问题-不可解决',
  '客户端问题-可解决',
  '源站问题',
  '上游网关问题',
]

interface ErrorStatsSearch {
  startTime?: number
  endTime?: number
  statusCode?: string
  model?: string
  channel?: string
  group?: string
  customerName?: string
  customerId?: string
  shortName?: string
  attribution?: string
  limit?: number
}

// Resolve the effective time range (defaults to the last 24 hours).
function resolveRange(search: ErrorStatsSearch): {
  startMs: number
  endMs: number
} {
  const endMs = search.endTime ?? Date.now()
  const startMs = search.startTime ?? endMs - DAY_IN_MS
  return { startMs, endMs }
}

function toParams(search: ErrorStatsSearch): ErrorStatsParams {
  const { startMs, endMs } = resolveRange(search)
  const statusCode = Number(search.statusCode)
  const channel = Number(search.channel)
  return {
    start_timestamp: Math.floor(startMs / 1000),
    end_timestamp: Math.floor(endMs / 1000),
    ...(Number.isFinite(statusCode) && statusCode > 0
      ? { status_code: statusCode }
      : {}),
    ...(search.model ? { model_name: search.model } : {}),
    ...(Number.isFinite(channel) && channel > 0 ? { channel } : {}),
    ...(search.group ? { group: search.group } : {}),
    ...(search.customerName ? { customer_name: search.customerName } : {}),
    ...(search.customerId ? { customer_id: search.customerId } : {}),
    ...(search.shortName ? { short_name: search.shortName } : {}),
    ...(search.attribution ? { attribution: search.attribution } : {}),
    limit: search.limit && search.limit > 0 ? search.limit : DEFAULT_LIMIT,
  }
}

function toDraft(search: ErrorStatsSearch): ErrorStatsFilterDraft {
  const { startMs, endMs } = resolveRange(search)
  return {
    start: new Date(startMs),
    end: new Date(endMs),
    statusCode: search.statusCode ?? '',
    model: search.model ?? '',
    channel: search.channel ?? '',
    group: search.group ?? '',
    customerName: search.customerName ?? '',
    customerId: search.customerId ?? '',
    shortName: search.shortName ?? '',
    attribution: search.attribution ?? '',
    limit: search.limit ? String(search.limit) : '',
  }
}

export function ErrorStats() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const navigate = useNavigate()
  const router = useRouter()
  const queryClient = useQueryClient()
  const search = route.useSearch() as ErrorStatsSearch

  const [draft, setDraft] = useState<ErrorStatsFilterDraft>(() =>
    toDraft(search)
  )

  const [viewMode, setViewMode] = useState<ErrorStatsGroupBy>('signature')

  const baseParams = useMemo(() => toParams(search), [search])
  const params = useMemo<ErrorStatsParams>(
    () => ({ ...baseParams, group_by: viewMode }),
    [baseParams, viewMode]
  )

  const statsQuery = useQuery({
    queryKey: ['error-stats', params],
    queryFn: () => getErrorStats(params),
    enabled: isAdmin,
  })

  const items = useMemo<ErrorStatItem[]>(
    () => statsQuery.data?.data ?? [],
    [statsQuery.data]
  )
  const total = useMemo(
    () => items.reduce((sum, item) => sum + item.count, 0),
    [items]
  )

  // Channel dropdown options derived from the channels that appear in the
  // current result set (manual id entry stays available via allowCustomValue).
  const channelOptions = useMemo(() => {
    const seen = new Map<number, string>()
    for (const item of items) {
      for (const channel of item.channels) {
        if (!seen.has(channel.channel_id)) {
          seen.set(channel.channel_id, channel.channel_name)
        }
      }
    }
    return [...seen.entries()].map(([id, name]) => ({
      value: String(id),
      label: name ? `${id} (${name})` : String(id),
    }))
  }, [items])

  // Every configured alias, so the short-name / attribution dropdowns offer
  // ALL previously-saved values, not just the ones present in the current rows.
  const aliasesQuery = useQuery({
    queryKey: ['error-aliases'],
    queryFn: getErrorAliases,
    enabled: isAdmin,
  })
  const aliases = useMemo(
    () => aliasesQuery.data?.data ?? [],
    [aliasesQuery.data]
  )

  // Distinct short names: every configured alias first, then any extra values
  // present in the current rows.
  const shortNameOptions = useMemo(() => {
    const seen = new Set<string>()
    const result: string[] = []
    const pushValue = (raw: string) => {
      const value = raw.trim()
      if (value && !seen.has(value)) {
        seen.add(value)
        result.push(value)
      }
    }
    for (const alias of aliases) pushValue(alias.short_name)
    for (const item of items) pushValue(item.short_name)
    return result
  }, [aliases, items])

  // Default attributions first, then every configured alias, then any extra
  // values present in the current rows.
  const attributionOptions = useMemo(() => {
    const seen = new Set<string>()
    const result: string[] = []
    const pushValue = (raw: string) => {
      const value = raw.trim()
      if (value && !seen.has(value)) {
        seen.add(value)
        result.push(value)
      }
    }
    for (const value of DEFAULT_ATTRIBUTIONS) pushValue(value)
    for (const alias of aliases) pushValue(alias.attribution)
    for (const item of items) pushValue(item.attribution)
    return result
  }, [aliases, items])

  const [savingHash, setSavingHash] = useState<string | null>(null)
  const [rulesOpen, setRulesOpen] = useState(false)
  const [suggestionsOpen, setSuggestionsOpen] = useState(false)
  const [pendingExtraRules, setPendingExtraRules] = useState<SignatureRule[]>(
    []
  )

  const handleAddSuggestions = (rules: SignatureRule[]) => {
    setPendingExtraRules(rules)
    setSuggestionsOpen(false)
    setRulesOpen(true)
  }

  const handleRulesOpenChange = (open: boolean) => {
    setRulesOpen(open)
    if (!open) setPendingExtraRules([])
  }

  const upsertMutation = useMutation({
    mutationFn: upsertErrorAlias,
    onSuccess: (res) => {
      if (res.success) {
        void queryClient.invalidateQueries({ queryKey: ['error-stats'] })
        void queryClient.invalidateQueries({ queryKey: ['error-aliases'] })
      }
    },
    onSettled: () => setSavingHash(null),
  })

  const deleteMutation = useMutation({
    mutationFn: deleteErrorAlias,
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Category cleared'))
        void queryClient.invalidateQueries({ queryKey: ['error-stats'] })
        void queryClient.invalidateQueries({ queryKey: ['error-aliases'] })
      }
    },
    onSettled: () => setSavingHash(null),
  })

  const handleDraftChange = (next: Partial<ErrorStatsFilterDraft>) =>
    setDraft((prev) => ({ ...prev, ...next }))

  const handleSearch = () => {
    void navigate({
      to: '/error-stats',
      search: {
        startTime: draft.start ? draft.start.getTime() : undefined,
        endTime: draft.end ? draft.end.getTime() : undefined,
        statusCode: draft.statusCode || undefined,
        model: draft.model || undefined,
        channel: draft.channel || undefined,
        group: draft.group || undefined,
        customerName: draft.customerName || undefined,
        customerId: draft.customerId || undefined,
        shortName: draft.shortName || undefined,
        attribution: draft.attribution || undefined,
        limit: draft.limit ? Number(draft.limit) || undefined : undefined,
      },
    })
  }

  const handleSaveAlias = (item: ErrorStatItem, next: AliasSaveInput) => {
    const shortName = next.short_name.trim()
    const attribution = next.attribution.trim()
    const note = next.note.trim()
    setSavingHash(item.signature_hash)

    // Clearing every field removes the alias entirely.
    if (!shortName && !attribution && !note) {
      deleteMutation.mutate(item.signature_hash)
      return
    }

    let message = t('Short name updated')
    if (next.field === 'attribution') {
      message = t('Attribution updated')
    } else if (next.field === 'note') {
      message = t('Note updated')
    }
    upsertMutation.mutate(
      {
        signature_hash: item.signature_hash,
        signature: item.signature,
        short_name: shortName,
        attribution,
        note,
      },
      {
        onSuccess: (res) => {
          if (res.success) toast.success(message)
        },
      }
    )
  }

  const handleDrillDown = (item: ErrorStatItem) => {
    const { startMs, endMs } = resolveRange(search)
    // Open the drill-down logs in a new browser tab instead of navigating in
    // place, so the stats view stays put while inspecting matching logs.
    const location = router.buildLocation({
      to: '/usage-logs/$section',
      params: { section: 'common' },
      search: {
        type: ['5'],
        errorSignatureHash: item.signature_hash,
        errorSignature: item.signature,
        startTime: startMs,
        endTime: endMs,
      },
    })
    window.open(location.href, '_blank', 'noopener,noreferrer')
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Detailed Error Stats')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        {!isAdmin ? (
          <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
            {t('This page is only available to administrators.')}
          </div>
        ) : (
          <div className='flex h-full min-h-0 flex-col gap-4'>
            <div className='flex flex-wrap items-end gap-3'>
              <ErrorStatsFilterBar
                draft={draft}
                onDraftChange={handleDraftChange}
                onSearch={handleSearch}
                isFetching={statsQuery.isFetching}
                channelOptions={channelOptions}
                shortNameOptions={shortNameOptions}
                attributionOptions={attributionOptions}
              />
            </div>
            <div className='min-h-0 flex-1'>
              <Tabs
                value={viewMode}
                onValueChange={(value) =>
                  setViewMode(value as ErrorStatsGroupBy)
                }
                className='flex h-full min-h-0 flex-col gap-3'
              >
                <div className='flex flex-wrap items-center justify-between gap-3'>
                  <TabsList>
                    <TabsTrigger value='signature'>
                      {t('By detailed error')}
                    </TabsTrigger>
                    <TabsTrigger value='short_name'>
                      {t('By error type')}
                    </TabsTrigger>
                    <TabsTrigger value='attribution'>
                      {t('By attribution')}
                    </TabsTrigger>
                  </TabsList>
                  <div className='flex items-center gap-3'>
                    <Button
                      type='button'
                      variant='outline'
                      className='h-8'
                      onClick={() => setSuggestionsOpen(true)}
                    >
                      <Lightbulb className='size-4' aria-hidden='true' />
                      {t('Recommend rules')}
                    </Button>
                    <Button
                      type='button'
                      variant='outline'
                      className='h-8'
                      onClick={() => setRulesOpen(true)}
                    >
                      <SlidersHorizontal className='size-4' aria-hidden='true' />
                      {t('Edit normalization rules')}
                    </Button>
                  </div>
                </div>
                <div className='min-h-0 flex-1'>
                  <ErrorStatsTable
                    items={items}
                    total={total}
                    isLoading={statsQuery.isLoading}
                    viewMode={viewMode}
                    savingHash={savingHash}
                    shortNameOptions={shortNameOptions}
                    attributionOptions={attributionOptions}
                    onSaveAlias={handleSaveAlias}
                    onDrillDown={handleDrillDown}
                  />
                </div>
              </Tabs>
            </div>
            <ErrorSignatureRuleSuggestionsDialog
              open={suggestionsOpen}
              onOpenChange={setSuggestionsOpen}
              params={baseParams}
              onAddRules={handleAddSuggestions}
            />
            <ErrorSignatureRulesDialog
              open={rulesOpen}
              onOpenChange={handleRulesOpenChange}
              initialExtraRules={pendingExtraRules}
            />
          </div>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
