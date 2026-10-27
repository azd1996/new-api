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
import { Edit, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
import { Button } from '@/components/ui/button'

import {
  SCHEDULE_TEMPLATE_KEY,
  createTemplate,
  deleteTemplate,
  listTemplates,
  updateTemplate,
} from './api'
import { TemplateDialog } from './template-dialog'
import type { ScheduleTemplate, ScheduleTemplatePayload } from './types'

export function TemplateSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<ScheduleTemplate | null>(null)

  const query = useQuery({
    queryKey: SCHEDULE_TEMPLATE_KEY,
    queryFn: listTemplates,
  })
  const rows = query.data?.data ?? []

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: SCHEDULE_TEMPLATE_KEY })
  }

  const saveMutation = useMutation({
    mutationFn: (payload: ScheduleTemplatePayload) =>
      editing ? updateTemplate(editing.id, payload) : createTemplate(payload),
    onSuccess: (res) => {
      if (!res.success) return
      toast.success(t('Saved successfully'))
      setDialogOpen(false)
      invalidate()
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteTemplate(id),
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
  const openEdit = (row: ScheduleTemplate) => {
    setEditing(row)
    setDialogOpen(true)
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between'>
        <span className='text-sm font-medium'>{t('Rule Templates')}</span>
        <Button variant='outline' size='sm' onClick={openCreate}>
          <Plus className='mr-1 h-3 w-3' />
          {t('Add Rule Template')}
        </Button>
      </div>
      <StaticDataTable
        tableClassName='min-w-max'
        data={rows}
        getRowKey={(row) => row.id}
        emptyClassName='text-muted-foreground py-8'
        emptyContent={t('No rule templates yet')}
        columns={[
          {
            id: 'id',
            header: t('ID'),
            cellClassName: 'text-muted-foreground',
            cell: (row) => row.id,
          },
          {
            id: 'name',
            header: t('Name'),
            cellClassName: 'font-medium',
            cell: (row) => row.name || '-',
          },
          {
            id: 'promql',
            header: t('PromQL'),
            cellClassName: 'max-w-xs font-mono text-xs',
            cell: (row) => row.promql || '-',
          },
          {
            id: 'result_label',
            header: t('Result label'),
            cell: (row) => row.result_label || '-',
          },
          {
            id: 'result_mode',
            header: t('Result mode'),
            cell: (row) => t(row.result_mode),
          },
          {
            id: 'description',
            header: t('Description'),
            cell: (row) => row.description || '-',
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
      <TemplateDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        template={editing}
        isSubmitting={saveMutation.isPending}
        onSubmit={(payload) => saveMutation.mutate(payload)}
      />
    </div>
  )
}
