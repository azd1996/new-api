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
import { Edit, Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

import { SettingsPageActionsPortal } from '../../components/settings-page-context'
import { SettingsSection } from '../../components/settings-section'
import { useUpdateOption } from '../../hooks/use-update-option'
import { TemplateEditorDialog } from './template-editor-dialog'
import type {
  OverrideTemplateItem,
  OverrideTemplateKind,
  OverrideTemplatesSettings,
} from './types'

function parseTemplates(jsonStr: string): OverrideTemplateItem[] {
  try {
    const arr = JSON.parse(jsonStr || '[]')
    if (!Array.isArray(arr)) return []
    return arr.map((item: Record<string, unknown>, i: number) => ({
      id: i,
      name: typeof item?.name === 'string' ? item.name : '',
      description:
        typeof item?.description === 'string' ? item.description : '',
      content: JSON.stringify(item?.content ?? null, null, 2),
    }))
  } catch {
    return []
  }
}

function serializeTemplates(items: OverrideTemplateItem[]): string {
  return JSON.stringify(
    items.map((item) => {
      const serialized: Record<string, unknown> = {
        name: item.name,
        content: JSON.parse(item.content),
      }
      if (item.description?.trim()) serialized.description = item.description
      return serialized
    })
  )
}

function normalizeSerialized(jsonStr: string): string {
  try {
    return JSON.stringify(JSON.parse(jsonStr || '[]'))
  } catch {
    return '[]'
  }
}

function TemplateTable(props: {
  title: string
  items: OverrideTemplateItem[]
  emptyContent: string
  onAdd: () => void
  onEdit: (item: OverrideTemplateItem) => void
  onDelete: (idx: number) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <span className='text-sm font-medium'>{props.title}</span>
        <Button variant='outline' size='sm' onClick={props.onAdd}>
          <Plus className='mr-1 h-3 w-3' />
          {t('Add Template')}
        </Button>
      </div>
      <StaticDataTable
        tableClassName='min-w-max'
        data={props.items}
        emptyClassName='text-muted-foreground py-8'
        emptyContent={props.emptyContent}
        columns={[
          {
            id: 'name',
            header: t('Name'),
            cellClassName: 'font-medium',
            cell: (item) => item.name || '-',
          },
          {
            id: 'description',
            header: t('Description'),
            cell: (item) => item.description || '-',
          },
          {
            id: 'actions',
            header: t('Actions'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (item, idx) => (
              <div className='flex justify-end gap-1'>
                <Button
                  variant='ghost'
                  size='icon'
                  className='h-7 w-7'
                  onClick={() => props.onEdit(item)}
                >
                  <Edit className='h-3 w-3' />
                </Button>
                <Button
                  variant='ghost'
                  size='icon'
                  className='h-7 w-7'
                  onClick={() => props.onDelete(idx)}
                >
                  <Trash2 className='h-3 w-3' />
                </Button>
              </div>
            ),
          },
        ]}
      />
    </div>
  )
}

interface EditorState {
  open: boolean
  kind: OverrideTemplateKind
  item: OverrideTemplateItem | null
}

interface Props {
  defaultValues: OverrideTemplatesSettings
}

export function OverrideTemplatesSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const [paramTemplates, setParamTemplates] = useState<OverrideTemplateItem[]>(
    () =>
      parseTemplates(
        props.defaultValues['override_template_setting.param_templates']
      )
  )
  const [retryTemplates, setRetryTemplates] = useState<OverrideTemplateItem[]>(
    () =>
      parseTemplates(
        props.defaultValues['override_template_setting.retry_templates']
      )
  )
  const [saving, setSaving] = useState(false)
  const [editor, setEditor] = useState<EditorState>({
    open: false,
    kind: 'param',
    item: null,
  })

  useEffect(() => {
    setParamTemplates(
      parseTemplates(
        props.defaultValues['override_template_setting.param_templates']
      )
    )
    setRetryTemplates(
      parseTemplates(
        props.defaultValues['override_template_setting.retry_templates']
      )
    )
  }, [props.defaultValues])

  const openEditor = (kind: OverrideTemplateKind, item: OverrideTemplateItem | null) => {
    setEditor({ open: true, kind, item })
  }

  const handleDelete = (kind: OverrideTemplateKind, idx: number) => {
    const setter = kind === 'param' ? setParamTemplates : setRetryTemplates
    setter((prev) =>
      prev.filter((_, i) => i !== idx).map((item, i) => ({ ...item, id: i }))
    )
    toast.success(t('Deleted successfully'))
  }

  const handleTemplateSave = (item: OverrideTemplateItem) => {
    const setter = editor.kind === 'param' ? setParamTemplates : setRetryTemplates
    setter((prev) => {
      const existIdx = prev.findIndex(
        (row) => row.id === item.id || row.name === editor.item?.name
      )
      if (existIdx >= 0) {
        const next = [...prev]
        next[existIdx] = { ...item, id: existIdx }
        return next
      }
      return [...prev, { ...item, id: prev.length }]
    })
    setEditor((prev) => ({ ...prev, item: null }))
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const updates: { key: string; value: string }[] = []

      const nextParam = serializeTemplates(paramTemplates)
      if (
        nextParam !==
        normalizeSerialized(
          props.defaultValues['override_template_setting.param_templates']
        )
      ) {
        updates.push({
          key: 'override_template_setting.param_templates',
          value: nextParam,
        })
      }

      const nextRetry = serializeTemplates(retryTemplates)
      if (
        nextRetry !==
        normalizeSerialized(
          props.defaultValues['override_template_setting.retry_templates']
        )
      ) {
        updates.push({
          key: 'override_template_setting.retry_templates',
          value: nextRetry,
        })
      }

      if (updates.length === 0) {
        toast.info(t('No changes'))
        return
      }

      for (const u of updates) {
        await updateOption.mutateAsync(u)
      }
      toast.success(t('Saved successfully'))
    } catch {
      toast.error(t('Failed to save'))
    } finally {
      setSaving(false)
    }
  }

  const existingNames =
    editor.kind === 'param'
      ? paramTemplates.map((item) => item.name)
      : retryTemplates.map((item) => item.name)

  return (
    <>
      <SettingsSection title={t('Override Templates')}>
        <Alert>
          <AlertDescription className='text-xs'>
            {t(
              'Define reusable parameter override and retry override templates. Channels can reference these templates by name.'
            )}
          </AlertDescription>
        </Alert>

        <TemplateTable
          title={t('Parameter Override Templates')}
          items={paramTemplates}
          emptyContent={t('No templates yet')}
          onAdd={() => openEditor('param', null)}
          onEdit={(item) => openEditor('param', item)}
          onDelete={(idx) => handleDelete('param', idx)}
        />

        <TemplateTable
          title={t('Retry Override Templates')}
          items={retryTemplates}
          emptyContent={t('No templates yet')}
          onAdd={() => openEditor('retry', null)}
          onEdit={(item) => openEditor('retry', item)}
          onDelete={(idx) => handleDelete('retry', idx)}
        />

        <SettingsPageActionsPortal>
          <Button size='sm' onClick={handleSave} disabled={saving}>
            {saving ? t('Saving...') : t('Save')}
          </Button>
        </SettingsPageActionsPortal>
      </SettingsSection>

      <TemplateEditorDialog
        open={editor.open}
        onOpenChange={(open) => setEditor((prev) => ({ ...prev, open }))}
        kind={editor.kind}
        item={editor.item}
        existingNames={existingNames}
        onSave={handleTemplateSave}
      />
    </>
  )
}
