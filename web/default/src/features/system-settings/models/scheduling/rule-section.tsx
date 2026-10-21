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
import { Edit, Eye, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import {
  SCHEDULE_RULE_KEY,
  SCHEDULE_TEMPLATE_KEY,
  createRule,
  deleteRule,
  listRules,
  listTemplates,
  previewSchedule,
  updateRule,
} from './api'
import { PreviewPanel } from './preview-panel'
import { RuleDialog } from './rule-dialog'
import type {
  SchedulePreviewResult,
  ScheduleRule,
  ScheduleRulePayload,
} from './types'

export function RuleSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<ScheduleRule | null>(null)
  const [preview, setPreview] = useState<SchedulePreviewResult | null>(null)
  const [previewingId, setPreviewingId] = useState<number | null>(null)

  const rulesQuery = useQuery({ queryKey: SCHEDULE_RULE_KEY, queryFn: listRules })
  const templatesQuery = useQuery({
    queryKey: SCHEDULE_TEMPLATE_KEY,
    queryFn: listTemplates,
  })
  const rows = rulesQuery.data?.data ?? []
  const templates = useMemo(
    () => templatesQuery.data?.data ?? [],
    [templatesQuery.data]
  )

  const templateNameById = useMemo(() => {
    const map = new Map<number, string>()
    for (const tpl of templates) map.set(tpl.id, tpl.name)
    return map
  }, [templates])

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: SCHEDULE_RULE_KEY })
  }

  const saveMutation = useMutation({
    mutationFn: (payload: ScheduleRulePayload) =>
      editing ? updateRule(editing.id, payload) : createRule(payload),
    onSuccess: (res) => {
      if (!res.success) return
      toast.success(t('Saved successfully'))
      setDialogOpen(false)
      invalidate()
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteRule(id),
    onSuccess: (res) => {
      if (!res.success) return
      toast.success(t('Deleted successfully'))
      invalidate()
    },
  })

  const openCreate = () => {
    setEditing(null)
    setDialogOpen(true)
  }
  const openEdit = (row: ScheduleRule) => {
    setEditing(row)
    setDialogOpen(true)
  }

  const runPreview = async (row: ScheduleRule) => {
    setPreviewingId(row.id)
    try {
      const res = await previewSchedule({ rule_id: row.id })
      if (res.success) setPreview(res.data)
    } finally {
      setPreviewingId(null)
    }
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <span className='text-sm font-medium'>{t('Rules')}</span>
        <Button variant='outline' size='sm' onClick={openCreate}>
          <Plus className='mr-1 h-3 w-3' />
          {t('Add Rule')}
        </Button>
      </div>
      <StaticDataTable
        tableClassName='min-w-max'
        data={rows}
        getRowKey={(row) => row.id}
        emptyClassName='text-muted-foreground py-8'
        emptyContent={t('No rules yet')}
        columns={[
          {
            id: 'id',
            header: t('ID'),
            cellClassName: 'text-muted-foreground',
            cell: (row) => row.id,
          },
          {
            id: 'target_type',
            header: t('Target type'),
            cell: (row) => t(row.target_type),
          },
          {
            id: 'target_value',
            header: t('Target value'),
            cellClassName: 'font-medium',
            cell: (row) => row.target_value || '-',
          },
          { id: 'model', header: t('Model'), cell: (row) => row.model || '-' },
          {
            id: 'template',
            header: t('Template'),
            cell: (row) =>
              templateNameById.get(row.template_id) ?? `#${row.template_id}`,
          },
          {
            id: 'params',
            header: t('Parameters'),
            cellClassName: 'max-w-xs font-mono text-xs',
            cell: (row) => row.params || '-',
          },
          {
            id: 'enabled',
            header: t('Enabled'),
            cell: (row) => (row.enabled ? t('Yes') : t('No')),
          },
          {
            id: 'actions',
            header: t('Actions'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (row) => (
              <div className='flex justify-end gap-1'>
                <Button
                  variant='ghost'
                  size='icon'
                  className='h-7 w-7'
                  aria-label={t('Preview')}
                  disabled={previewingId === row.id}
                  onClick={() => runPreview(row)}
                >
                  <Eye className='h-3 w-3' />
                </Button>
                <Button
                  variant='ghost'
                  size='icon'
                  className='h-7 w-7'
                  aria-label={t('Edit')}
                  onClick={() => openEdit(row)}
                >
                  <Edit className='h-3 w-3' />
                </Button>
                <Button
                  variant='ghost'
                  size='icon'
                  className='h-7 w-7'
                  aria-label={t('Delete')}
                  disabled={deleteMutation.isPending}
                  onClick={() => deleteMutation.mutate(row.id)}
                >
                  <Trash2 className='h-3 w-3' />
                </Button>
              </div>
            ),
          },
        ]}
      />
      <RuleDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        rule={editing}
        templates={templates}
        isSubmitting={saveMutation.isPending}
        onSubmit={(payload) => saveMutation.mutate(payload)}
      />
      <Dialog
        open={preview !== null}
        onOpenChange={(open) => {
          if (!open) setPreview(null)
        }}
        title={t('Preview')}
        contentClassName='max-w-2xl'
      >
        {preview && <PreviewPanel result={preview} />}
      </Dialog>
    </div>
  )
}
