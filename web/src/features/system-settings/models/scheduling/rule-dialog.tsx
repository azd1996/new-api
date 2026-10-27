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
import { useEffect, useMemo, useState } from 'react'
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
import { Switch } from '@/components/ui/switch'

import { previewSchedule } from './api'
import {
  TARGET_TYPES,
  extractPlaceholderNames,
  parseRuleParams,
} from './constants'
import { PreviewPanel } from './preview-panel'
import type {
  SchedulePreviewResult,
  ScheduleRule,
  ScheduleRulePayload,
  ScheduleTargetType,
  ScheduleTemplate,
} from './types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  rule: ScheduleRule | null
  templates: ScheduleTemplate[]
  onSubmit: (payload: ScheduleRulePayload) => void
  isSubmitting: boolean
}

export function RuleDialog(props: Props) {
  const { t } = useTranslation()
  const isEdit = !!props.rule
  const [targetType, setTargetType] = useState<ScheduleTargetType>('group')
  const [targetValue, setTargetValue] = useState('')
  const [model, setModel] = useState('')
  const [templateId, setTemplateId] = useState<number | null>(null)
  const [params, setParams] = useState<Record<string, string>>({})
  const [enabled, setEnabled] = useState(true)
  const [preview, setPreview] = useState<SchedulePreviewResult | null>(null)
  const [previewing, setPreviewing] = useState(false)

  useEffect(() => {
    if (!props.open) return
    setTargetType(props.rule?.target_type ?? 'group')
    setTargetValue(props.rule?.target_value ?? '')
    setModel(props.rule?.model ?? '')
    setTemplateId(props.rule?.template_id ?? null)
    setParams(props.rule ? parseRuleParams(props.rule.params) : {})
    setEnabled(props.rule?.enabled ?? true)
    setPreview(null)
    setPreviewing(false)
  }, [props.open, props.rule])

  const selectedTemplate = useMemo(
    () => props.templates.find((tpl) => tpl.id === templateId) ?? null,
    [props.templates, templateId]
  )
  const placeholders = useMemo(
    () =>
      selectedTemplate ? extractPlaceholderNames(selectedTemplate.promql) : [],
    [selectedTemplate]
  )

  const collectParams = (): Record<string, string> => {
    const result: Record<string, string> = {}
    for (const name of placeholders) {
      result[name] = params[name] ?? ''
    }
    return result
  }

  const handleSave = () => {
    if (!targetValue.trim()) {
      toast.error(t('Target value is required'))
      return
    }
    if (!model.trim()) {
      toast.error(t('Model is required'))
      return
    }
    if (templateId === null) {
      toast.error(t('Please select a template'))
      return
    }
    props.onSubmit({
      target_type: targetType,
      target_value: targetValue.trim(),
      model: model.trim(),
      template_id: templateId,
      params: JSON.stringify(collectParams()),
      enabled,
    })
  }

  const handlePreview = async () => {
    if (templateId === null) {
      toast.error(t('Please select a template'))
      return
    }
    setPreviewing(true)
    try {
      const res = await previewSchedule({
        template_id: templateId,
        params: collectParams(),
      })
      if (res.success) setPreview(res.data)
    } finally {
      setPreviewing(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEdit ? t('Edit Rule') : t('Add Rule')}
      contentClassName='max-w-2xl'
      bodyClassName='pr-2'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={handlePreview}
            disabled={previewing}
          >
            {previewing ? t('Previewing...') : t('Preview')}
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
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='grid gap-1.5'>
            <Label>{t('Target type')}</Label>
            <Select
              items={TARGET_TYPES.map((type) => ({
                value: type,
                label: t(type),
              }))}
              value={targetType}
              onValueChange={(value) => {
                if (value === null) return
                setTargetType(value as ScheduleTargetType)
              }}
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {TARGET_TYPES.map((type) => (
                    <SelectItem key={type} value={type}>
                      {t(type)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-1.5'>
            <Label>{t('Target value')} *</Label>
            <Input
              placeholder='default'
              value={targetValue}
              onChange={(e) => setTargetValue(e.target.value)}
            />
          </div>
        </div>
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='grid gap-1.5'>
            <Label>{t('Model')} *</Label>
            <Input
              placeholder='claude-opus'
              value={model}
              onChange={(e) => setModel(e.target.value)}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label>{t('Template')} *</Label>
            <Select
              items={props.templates.map((tpl) => ({
                value: String(tpl.id),
                label: tpl.name,
              }))}
              value={templateId === null ? '' : String(templateId)}
              onValueChange={(value) => {
                if (value === null) return
                setTemplateId(Number(value))
              }}
            >
              <SelectTrigger className='w-full'>
                <SelectValue placeholder={t('Select a template')} />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {props.templates.map((tpl) => (
                    <SelectItem key={tpl.id} value={String(tpl.id)}>
                      {tpl.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
        </div>

        {placeholders.length > 0 && (
          <div className='grid gap-2 rounded-md border p-3'>
            <span className='text-xs font-medium'>{t('Parameters')}</span>
            {placeholders.map((name) => (
              <div key={name} className='grid gap-1.5'>
                <Label className='font-mono text-xs'>{name}</Label>
                <Input
                  value={params[name] ?? ''}
                  onChange={(e) =>
                    setParams((prev) => ({ ...prev, [name]: e.target.value }))
                  }
                />
              </div>
            ))}
          </div>
        )}

        <div className='flex items-center gap-2'>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
          <Label>{t('Enabled')}</Label>
        </div>

        {preview && <PreviewPanel result={preview} />}
      </div>
    </Dialog>
  )
}
