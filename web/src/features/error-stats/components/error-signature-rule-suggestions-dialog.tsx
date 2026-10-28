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
import { Braces, Copy, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import { getErrorSignatureRuleSuggestions } from '../api'
import type { ErrorStatsParams, SignatureRule, SuggestedRule } from '../types'

interface ErrorSignatureRuleSuggestionsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  // The current resolved filter params the table is showing. Included in the
  // query key so suggestions refetch when the window changes.
  params: ErrorStatsParams
  // Append the given rule(s) into the normalization rules editor.
  onAddRules: (rules: SignatureRule[]) => void
}

function toSignatureRule(suggestion: SuggestedRule): SignatureRule {
  return {
    name: suggestion.name,
    pattern: suggestion.pattern,
    replacement: suggestion.replacement,
    enabled: true,
  }
}

export function ErrorSignatureRuleSuggestionsDialog(
  props: ErrorSignatureRuleSuggestionsDialogProps
) {
  const { t } = useTranslation()

  const suggestionsQuery = useQuery({
    queryKey: ['error-signature-rule-suggestions', props.params],
    queryFn: () => getErrorSignatureRuleSuggestions(props.params),
    enabled: props.open,
    staleTime: 0,
    refetchOnMount: 'always',
  })

  const suggestions = suggestionsQuery.data?.data ?? []

  const handleCopyPattern = async (suggestion: SuggestedRule) => {
    const ok = await copyToClipboard(suggestion.pattern)
    if (ok) {
      toast.success(t('Copied'))
    }
  }

  const handleCopyJson = async (suggestion: SuggestedRule) => {
    const payload = JSON.stringify(toSignatureRule(suggestion))
    const ok = await copyToClipboard(payload)
    if (ok) {
      toast.success(t('Copied'))
    }
  }

  const handleAddAll = () => {
    if (suggestions.length === 0) return
    props.onAddRules(suggestions.map(toSignatureRule))
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Suggested rules')}
      contentClassName='sm:max-w-3xl'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Close')}
          </Button>
          <Button
            type='button'
            size='sm'
            onClick={handleAddAll}
            disabled={suggestions.length === 0}
          >
            <Plus className='size-3.5' aria-hidden='true' />
            {t('Add all to rules')}
          </Button>
        </>
      }
    >
      <div className='space-y-3'>
        {suggestionsQuery.isLoading && (
          <div className='text-muted-foreground py-6 text-center text-sm'>
            {t('Loading...')}
          </div>
        )}

        {!suggestionsQuery.isLoading && suggestions.length === 0 && (
          <div className='text-muted-foreground py-6 text-center text-sm'>
            {t('No suggestions — signatures look clean.')}
          </div>
        )}

        {!suggestionsQuery.isLoading &&
          suggestions.map((suggestion) => (
            <div
              key={suggestion.pattern}
              className='bg-muted/30 flex flex-col gap-3 rounded-lg border p-3'
            >
              <div className='flex items-start justify-between gap-2'>
                <span className='text-sm font-medium break-all'>
                  {suggestion.name}
                </span>
                <Badge variant='secondary' className='shrink-0'>
                  {t('Matched {{count}}', { count: suggestion.match_count })}
                </Badge>
              </div>

              <div className='grid gap-2 sm:grid-cols-2'>
                <div className='space-y-1'>
                  <span className='text-muted-foreground text-xs'>
                    {t('Pattern')}
                  </span>
                  <pre className='bg-background overflow-x-auto rounded-md border p-2 font-mono text-xs break-all whitespace-pre-wrap'>
                    {suggestion.pattern}
                  </pre>
                </div>
                <div className='space-y-1'>
                  <span className='text-muted-foreground text-xs'>
                    {t('Replacement')}
                  </span>
                  <pre className='bg-background overflow-x-auto rounded-md border p-2 font-mono text-xs break-all whitespace-pre-wrap'>
                    {suggestion.replacement}
                  </pre>
                </div>
              </div>

              <div className='space-y-1'>
                <span className='text-muted-foreground text-xs'>
                  {t('Example')}
                </span>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <p className='text-muted-foreground line-clamp-2 cursor-help font-mono text-xs break-all' />
                    }
                  >
                    {suggestion.example}
                  </TooltipTrigger>
                  <TooltipContent className='max-w-md break-all whitespace-pre-wrap'>
                    {suggestion.example}
                  </TooltipContent>
                </Tooltip>
              </div>

              <div className='flex flex-wrap items-center gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => void handleCopyPattern(suggestion)}
                >
                  <Copy className='size-3.5' aria-hidden='true' />
                  {t('Copy pattern')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => void handleCopyJson(suggestion)}
                >
                  <Braces className='size-3.5' aria-hidden='true' />
                  {t('Copy as JSON')}
                </Button>
                <Button
                  type='button'
                  size='sm'
                  onClick={() => props.onAddRules([toSignatureRule(suggestion)])}
                >
                  <Plus className='size-3.5' aria-hidden='true' />
                  {t('Add to rules')}
                </Button>
              </div>
            </div>
          ))}
      </div>
    </Dialog>
  )
}
