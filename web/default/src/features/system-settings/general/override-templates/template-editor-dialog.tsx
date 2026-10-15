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
import { Code, Wand2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ParamOverrideEditorDialog } from '@/features/channels/components/dialogs/param-override-editor-dialog'

import type { OverrideTemplateItem, OverrideTemplateKind } from './types'

// Mirror of the sample retry rule array used by the channel retry-rules-section.
const RETRY_TEMPLATE_SAMPLE = JSON.stringify(
  [
    {
      description: 'Strip thinking blocks on a thinking-related 400, then retry',
      phase2_response_conditions: {
        conditions: [
          { path: 'status_code', mode: 'full', value: 400 },
          { path: 'error_message', mode: 'contains', value: 'thinking' },
        ],
        logic: 'AND',
      },
      phase3_request_rewrite: [
        {
          mode: 'prune_objects',
          path: 'messages',
          value: { where: { type: 'thinking' } },
        },
        {
          mode: 'prune_objects',
          path: 'messages',
          value: { where: { type: 'redacted_thinking' } },
        },
      ],
      phase4_retry_action: 'retry_same_channel',
    },
  ],
  null,
  2
)

const PARAM_TEMPLATE_SAMPLE = JSON.stringify(
  {
    operations: [
      {
        path: 'temperature',
        mode: 'set',
        value: 0.7,
        conditions: [{ path: 'model', mode: 'prefix', value: 'gpt' }],
        logic: 'AND',
      },
    ],
  },
  null,
  2
)

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  kind: OverrideTemplateKind
  item: OverrideTemplateItem | null
  existingNames: string[]
  onSave: (item: OverrideTemplateItem) => void
}

export function TemplateEditorDialog(props: Props) {
  const { t } = useTranslation()
  const isEdit = !!props.item?.name
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [content, setContent] = useState('')
  const [visualEditorOpen, setVisualEditorOpen] = useState(false)

  useEffect(() => {
    if (!props.open) return
    setName(props.item?.name ?? '')
    setDescription(props.item?.description ?? '')
    setContent(props.item?.content ?? '')
    setVisualEditorOpen(false)
  }, [props.open, props.item])

  const handleSave = () => {
    const trimmedName = name.trim()
    if (!trimmedName) {
      toast.error(t('Template name is required'))
      return
    }
    const normalizedExisting = props.existingNames
      .map((n) => n.trim())
      .filter((n) => n.length > 0 && n !== props.item?.name?.trim())
    if (normalizedExisting.includes(trimmedName)) {
      toast.error(t('Template name already exists'))
      return
    }

    let parsed: unknown
    try {
      parsed = JSON.parse(content)
    } catch {
      toast.error(t('Content must be valid JSON'))
      return
    }

    if (props.kind === 'retry') {
      if (!Array.isArray(parsed)) {
        toast.error(t('Retry template content must be a JSON array'))
        return
      }
    } else if (typeof parsed !== 'object' || parsed === null) {
      toast.error(t('Parameter template content must be a JSON object or array'))
      return
    }

    props.onSave({
      id: props.item?.id,
      name: trimmedName,
      description: description.trim(),
      content,
    })
    props.onOpenChange(false)
  }

  const fillSample = () => {
    setContent(
      props.kind === 'retry' ? RETRY_TEMPLATE_SAMPLE : PARAM_TEMPLATE_SAMPLE
    )
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEdit ? t('Edit Template') : t('Add Template')}
      contentClassName='max-w-2xl'
      contentHeight='auto'
      bodyClassName='pr-2'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleSave}>
            {t('Save')}
          </Button>
        </>
      }
    >
      <div className='min-w-0 space-y-4'>
        <div className='grid gap-1.5'>
          <Label>{t('Template name')} *</Label>
          <Input
            placeholder={t('Template name')}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        <div className='grid gap-1.5'>
          <Label>{t('Description')}</Label>
          <Input
            placeholder={t('Optional description')}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>

        <div className='grid gap-1.5'>
          <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
            <Label>{t('Content')}</Label>
            <div className='flex flex-wrap gap-2'>
              {props.kind === 'param' && (
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={() => setVisualEditorOpen(true)}
                >
                  <Wand2 className='mr-1 h-3 w-3' />
                  {t('Visual edit')}
                </Button>
              )}
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={fillSample}
              >
                <Code className='mr-1 h-3 w-3' />
                {t('Fill Template')}
              </Button>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => setContent('')}
              >
                {t('Clear')}
              </Button>
            </div>
          </div>
          <Textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            rows={12}
            placeholder={
              props.kind === 'retry'
                ? t(
                    'Retry override rules as a JSON array. Leave empty to disable.'
                  )
                : t('Parameter override object, e.g. {"operations": [...]}')
            }
            className='max-h-96 min-h-40 resize-y overflow-auto font-mono text-xs'
          />
        </div>
      </div>

      {props.kind === 'param' && visualEditorOpen && (
        <ParamOverrideEditorDialog
          open={visualEditorOpen}
          value={content}
          onOpenChange={setVisualEditorOpen}
          onSave={(nextValue) => setContent(nextValue)}
        />
      )}
    </Dialog>
  )
}
