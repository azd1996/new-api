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
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'

import { RESULT_MODES } from './constants'
import type {
  ScheduleResultMode,
  ScheduleTemplate,
  ScheduleTemplatePayload,
} from './types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  template: ScheduleTemplate | null
  onSubmit: (payload: ScheduleTemplatePayload) => void
  isSubmitting: boolean
}

export function TemplateDialog(props: Props) {
  const { t } = useTranslation()
  const isEdit = !!props.template
  const [name, setName] = useState('')
  const [promql, setPromql] = useState('')
  const [resultLabel, setResultLabel] = useState('')
  const [resultMode, setResultMode] = useState<ScheduleResultMode>('rank')
  const [description, setDescription] = useState('')

  useEffect(() => {
    if (!props.open) return
    setName(props.template?.name ?? '')
    setPromql(props.template?.promql ?? '')
    setResultLabel(props.template?.result_label ?? '')
    setResultMode(props.template?.result_mode ?? 'rank')
    setDescription(props.template?.description ?? '')
  }, [props.open, props.template])

  const handleSave = () => {
    if (!name.trim()) {
      toast.error(t('Template name is required'))
      return
    }
    if (!promql.trim()) {
      toast.error(t('PromQL is required'))
      return
    }
    props.onSubmit({
      name: name.trim(),
      promql: promql.trim(),
      result_label: resultLabel.trim(),
      result_mode: resultMode,
      description: description.trim(),
    })
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEdit ? t('Edit Rule Template') : t('Add Rule Template')}
      contentClassName='max-w-2xl'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={handleSave}
            disabled={props.isSubmitting}
          >
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='min-w-0 space-y-4'>
        <div className='grid gap-1.5'>
          <Label>{t('Name')} *</Label>
          <Input
            placeholder='latency-rank'
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('PromQL')} *</Label>
          <Textarea
            rows={6}
            placeholder='topk(__TOPK__, rate(request_duration_seconds{model="__MODEL__"}[__TIMERANGE__]))'
            value={promql}
            onChange={(e) => setPromql(e.target.value)}
            className='max-h-72 min-h-32 resize-y overflow-auto font-mono text-xs'
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Use __PLACEHOLDER__ tokens (e.g. __MODEL__, __TIMERANGE__) that rules will fill in.'
            )}
          </p>
        </div>
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='grid gap-1.5'>
            <Label>{t('Result label')}</Label>
            <Input
              placeholder='channel_id'
              value={resultLabel}
              onChange={(e) => setResultLabel(e.target.value)}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label>{t('Result mode')}</Label>
            <Select
              items={RESULT_MODES.map((mode) => ({
                value: mode,
                label: t(mode),
              }))}
              value={resultMode}
              onValueChange={(value) => {
                if (value === null) return
                setResultMode(value as ScheduleResultMode)
              }}
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {RESULT_MODES.map((mode) => (
                    <SelectItem key={mode} value={mode}>
                      {t(mode)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Description')}</Label>
          <Input
            placeholder={t('Optional description')}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
      </div>
    </Dialog>
  )
}
