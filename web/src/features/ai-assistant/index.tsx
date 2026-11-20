import { Bot, ChevronDown, ChevronUp, Loader2 } from 'lucide-react'
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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Playground } from '@/features/playground'
import { useAuthStore } from '@/stores/auth-store'

import { useAIAssistantConfig } from './api'

const SYSTEM_PROMPT_STORAGE_KEY = 'ai_assistant_system_prompt'

export function AIAssistant() {
  const { t } = useTranslation()
  const { data, isLoading, isError } = useAIAssistantConfig()
  const userId = useAuthStore((s) => s.auth.user?.id)
  const [systemPrompt, setSystemPrompt] = useState('')
  const [showSystemPrompt, setShowSystemPrompt] = useState(false)

  useEffect(() => {
    if (userId == null) return
    setSystemPrompt(
      localStorage.getItem(`${SYSTEM_PROMPT_STORAGE_KEY}:${userId}`) ?? ''
    )
  }, [userId])

  const handleSystemPromptChange = (value: string) => {
    setSystemPrompt(value)
    if (userId != null) {
      localStorage.setItem(`${SYSTEM_PROMPT_STORAGE_KEY}:${userId}`, value)
    }
  }

  if (isLoading) {
    return (
      <div className='flex size-full items-center justify-center'>
        <Loader2 className='text-muted-foreground size-6 animate-spin' />
      </div>
    )
  }

  if (isError || !data?.enabled || !data.group) {
    return (
      <div className='flex size-full flex-col items-center justify-center gap-3 p-8 text-center'>
        <Bot className='text-muted-foreground size-10' />
        <div>
          <p className='text-foreground font-medium'>
            {t('AI assistant is not available')}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Ask an administrator to enable the AI assistant and configure its channel group.'
            )}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className='flex size-full min-h-0 flex-col'>
      <div className='flex flex-col gap-3 border-b px-4 py-3 md:px-6'>
        <div className='flex items-center justify-between gap-4'>
          <div className='flex min-w-0 items-center gap-2.5'>
            <Bot className='text-primary size-5 shrink-0' />
            <div className='min-w-0'>
              <p className='text-sm leading-tight font-semibold'>
                {t('AI Assistant')}
              </p>
              <p className='text-muted-foreground truncate text-xs'>
                {t(
                  'Ask the assistant model questions using the configured channel group.'
                )}
              </p>
            </div>
          </div>
          <Button
            aria-expanded={showSystemPrompt}
            onClick={() => setShowSystemPrompt((value) => !value)}
            size='sm'
            type='button'
            variant='outline'
          >
            {t('System prompt')}
            {showSystemPrompt ? (
              <ChevronUp className='size-4' />
            ) : (
              <ChevronDown className='size-4' />
            )}
          </Button>
        </div>
        {showSystemPrompt && (
          <Textarea
            aria-label={t('System prompt')}
            className='min-h-20'
            onChange={(event) => handleSystemPromptChange(event.target.value)}
            placeholder={t('Enter a system prompt (optional)')}
            value={systemPrompt}
          />
        )}
      </div>
      <div className='min-h-0 flex-1'>
        <Playground
          defaultModel={data.default_model}
          fixedGroup={data.group}
          storageNamespace='ai-assistant'
          systemPrompt={systemPrompt}
        />
      </div>
    </div>
  )
}
