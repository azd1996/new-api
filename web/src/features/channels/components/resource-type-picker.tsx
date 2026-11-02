import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/server-error-message'

import {
  createResourceType,
  getResourceTypes,
  type ResourceType,
} from '../api'

type ResourceTypePickerProps = {
  value: number | null
  disabled?: boolean
  onChange: (value: number | null) => void
}

const resourceTypeQueryKey = ['resource-types'] as const

export function ResourceTypePicker(props: ResourceTypePickerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [newName, setNewName] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const resourceTypesQuery = useQuery({
    queryKey: resourceTypeQueryKey,
    queryFn: getResourceTypes,
  })
  const createMutation = useMutation({
    mutationFn: createResourceType,
    onSuccess: async (resourceType: ResourceType) => {
      await queryClient.invalidateQueries({ queryKey: resourceTypeQueryKey })
      props.onChange(resourceType.id)
      setNewName('')
      setDialogOpen(false)
    },
    onError: (error) => handleServerError(error, t('Unable to create resource type')),
  })
  const options =
    [
      { label: t('None'), value: '' },
      ...(resourceTypesQuery.data?.map((resourceType) => ({
        label: resourceType.name,
        value: String(resourceType.id),
      })) ?? []),
    ]

  return (
    <div className='flex min-w-0 items-center gap-2'>
      <Combobox
        options={options}
        value={props.value === null ? '' : String(props.value)}
        onValueChange={(value) => props.onChange(value ? Number(value) : null)}
        placeholder={t('Select a resource type')}
        emptyText={t('No resource types')}
        disabled={props.disabled || resourceTypesQuery.isLoading}
        className='flex-1'
        aria-label={t('Resource Type')}
      />
      <Button
        type='button'
        variant='outline'
        size='icon'
        aria-label={t('Add resource type')}
        title={t('Add resource type')}
        disabled={props.disabled}
        onClick={() => setDialogOpen(true)}
      >
        <Plus aria-hidden='true' />
      </Button>
      <Dialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title={t('Add resource type')}
        footer={
          <>
            <Button
              type='button'
              variant='outline'
              onClick={() => setDialogOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              disabled={!newName.trim() || createMutation.isPending}
              onClick={() => createMutation.mutate(newName.trim())}
            >
              {t('Add')}
            </Button>
          </>
        }
      >
        <div className='grid gap-2'>
          <Label htmlFor='new-resource-type'>{t('Resource type name')}</Label>
          <Input
            id='new-resource-type'
            value={newName}
            maxLength={128}
            onChange={(event) => setNewName(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && newName.trim()) {
                event.preventDefault()
                createMutation.mutate(newName.trim())
              }
            }}
          />
        </div>
      </Dialog>
    </div>
  )
}
