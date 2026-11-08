import { useState } from 'react'
import { Trash2 } from 'lucide-react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import type { CustomerFormValues } from '../lib/customer-form'

export function CustomerSessionRoutingPolicy() {
  const { t } = useTranslation()
  const form = useFormContext<CustomerFormValues>()
  const rules = useWatch({
    control: form.control,
    name: 'session_routing_model_prefixes',
  })
  const [prefix, setPrefix] = useState('')
  const [prefixToRemove, setPrefixToRemove] = useState<string | null>(null)
  const entries = Object.entries(rules ?? {})

  const updateRules = (next: Record<string, boolean>) => {
    form.setValue('session_routing_model_prefixes', next, {
      shouldDirty: true,
    })
  }

  const addPrefix = () => {
    const normalized = prefix.trim()
    if (!normalized || Object.hasOwn(rules ?? {}, normalized)) return
    updateRules({ ...rules, [normalized]: false })
    setPrefix('')
  }

  const removePrefix = () => {
    if (!prefixToRemove) return
    const next = { ...rules }
    delete next[prefixToRemove]
    updateRules(next)
    setPrefixToRemove(null)
  }

  return (
    <>
      <SideDrawerSection>
        <SideDrawerSectionHeader title={t('Session routing')} />
        <p className='text-muted-foreground text-sm'>
          {t(
            'Session routing is disabled by default. Add model prefixes to enable it for this customer.'
          )}
        </p>
        <div className='flex items-center gap-2'>
          <Input
            value={prefix}
            placeholder={t('Model name prefix (e.g. claude-)')}
            onChange={(event) => setPrefix(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                addPrefix()
              }
            }}
          />
          <Button type='button' variant='outline' onClick={addPrefix}>
            {t('Add prefix')}
          </Button>
        </div>
        {entries.length === 0 ? (
          <p className='text-muted-foreground text-sm'>
            {t('No model prefixes configured.')}
          </p>
        ) : (
          <div className='space-y-2'>
            {entries.map(([modelPrefix, enabled]) => (
              <div
                key={modelPrefix}
                className='flex items-center justify-between gap-3 rounded-md border p-3'
              >
                <div className='flex min-w-0 flex-1 items-center gap-3'>
                  <Label
                    htmlFor={`session-routing-${modelPrefix}`}
                    className='truncate'
                  >
                    {modelPrefix}
                  </Label>
                  <Switch
                    id={`session-routing-${modelPrefix}`}
                    checked={enabled}
                    onCheckedChange={(checked) =>
                      updateRules({ ...rules, [modelPrefix]: checked })
                    }
                    aria-label={t('Enable session routing for {{prefix}}', {
                      prefix: modelPrefix,
                    })}
                  />
                </div>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  aria-label={t('Remove')}
                  onClick={() => setPrefixToRemove(modelPrefix)}
                >
                  <Trash2 aria-hidden='true' />
                </Button>
              </div>
            ))}
          </div>
        )}
      </SideDrawerSection>
      <ConfirmDialog
        open={prefixToRemove !== null}
        onOpenChange={(open) => {
          if (!open) setPrefixToRemove(null)
        }}
        title={t('Confirm delete')}
        desc={t('Are you sure?')}
        confirmText={t('Confirm delete')}
        destructive
        handleConfirm={removePrefix}
      />
    </>
  )
}
