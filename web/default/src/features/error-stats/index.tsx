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
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { Lightbulb, SlidersHorizontal } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { useIsAdmin } from '@/hooks/use-admin'

import { deleteErrorAlias, getErrorStats, upsertErrorAlias } from './api'
import { ErrorSignatureRuleSuggestionsDialog } from './components/error-signature-rule-suggestions-dialog'
import { ErrorSignatureRulesDialog } from './components/error-signature-rules-dialog'
import {
  ErrorStatsFilterBar,
  type ErrorStatsFilterDraft,
} from './components/error-stats-filter-bar'
import { ErrorStatsTable } from './components/error-stats-table'
import type {
  ErrorStatItem,
  ErrorStatsParams,
  SignatureRule,
} from './types'

const route = getRouteApi('/_authenticated/error-stats/')

const DAY_IN_MS = 24 * 60 * 60 * 1000
const DEFAULT_LIMIT = 200

interface ErrorStatsSearch {
  startTime?: number
  endTime?: number
  statusCode?: string
  model?: string
  channel?: string
  group?: string
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
    limit: search.limit ? String(search.limit) : '',
  }
}

export function ErrorStats() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const search = route.useSearch() as ErrorStatsSearch

  const [draft, setDraft] = useState<ErrorStatsFilterDraft>(() =>
    toDraft(search)
  )

  const params = useMemo(() => toParams(search), [search])

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
        toast.success(t('Short name updated'))
        void queryClient.invalidateQueries({ queryKey: ['error-stats'] })
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
        limit: draft.limit ? Number(draft.limit) || undefined : undefined,
      },
    })
  }

  const handleSaveShortName = (item: ErrorStatItem, shortName: string) => {
    setSavingHash(item.signature_hash)
    upsertMutation.mutate({
      signature_hash: item.signature_hash,
      signature: item.signature,
      short_name: shortName,
    })
  }

  const handleClearShortName = (item: ErrorStatItem) => {
    setSavingHash(item.signature_hash)
    deleteMutation.mutate(item.signature_hash)
  }

  const handleDrillDown = (item: ErrorStatItem) => {
    const { startMs, endMs } = resolveRange(search)
    void navigate({
      to: '/usage-logs/$section',
      params: { section: 'common' },
      search: {
        type: ['5'],
        errorSignatureHash: item.signature_hash,
        startTime: startMs,
        endTime: endMs,
      },
    })
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
            <div className='flex flex-wrap items-end justify-between gap-3'>
              <ErrorStatsFilterBar
                draft={draft}
                onDraftChange={handleDraftChange}
                onSearch={handleSearch}
                isFetching={statsQuery.isFetching}
              />
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
            <div className='min-h-0 flex-1'>
              <ErrorStatsTable
                items={items}
                total={total}
                isLoading={statsQuery.isLoading}
                savingHash={savingHash}
                onSaveShortName={handleSaveShortName}
                onClearShortName={handleClearShortName}
                onDrillDown={handleDrillDown}
              />
            </div>
            <ErrorSignatureRuleSuggestionsDialog
              open={suggestionsOpen}
              onOpenChange={setSuggestionsOpen}
              params={params}
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
