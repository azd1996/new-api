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
import { Switch } from '@/components/ui/switch'

import { testDatasource } from './api'
import type { ScheduleDatasource, ScheduleDatasourcePayload } from './types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  datasource: ScheduleDatasource | null
  onSubmit: (payload: ScheduleDatasourcePayload) => void
  isSubmitting: boolean
}

export function DatasourceDialog(props: Props) {
  const { t } = useTranslation()
  const isEdit = !!props.datasource
  const [name, setName] = useState('')
  const [baseUrl, setBaseUrl] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [timeoutSeconds, setTimeoutSeconds] = useState(30)
  const [testing, setTesting] = useState(false)

  useEffect(() => {
    if (!props.open) return
    setName(props.datasource?.name ?? '')
    setBaseUrl(props.datasource?.base_url ?? '')
    setUsername(props.datasource?.username ?? '')
    setPassword('')
    setEnabled(props.datasource?.enabled ?? true)
    setTimeoutSeconds(props.datasource?.timeout_seconds ?? 30)
    setTesting(false)
  }, [props.open, props.datasource])

  const buildPayload = (): ScheduleDatasourcePayload => ({
    name: name.trim(),
    base_url: baseUrl.trim(),
    username: username.trim(),
    password,
    enabled,
    timeout_seconds: Number(timeoutSeconds) || 0,
  })

  const validate = () => {
    if (!name.trim()) {
      toast.error(t('Data source name is required'))
      return false
    }
    if (!baseUrl.trim()) {
      toast.error(t('Base URL is required'))
      return false
    }
    return true
  }

  const handleSave = () => {
    if (!validate()) return
    props.onSubmit(buildPayload())
  }

  const handleTest = async () => {
    if (!baseUrl.trim()) {
      toast.error(t('Base URL is required'))
      return
    }
    setTesting(true)
    try {
      const res = await testDatasource({
        id: props.datasource?.id,
        base_url: baseUrl.trim(),
        username: username.trim(),
        password,
        timeout_seconds: Number(timeoutSeconds) || 0,
      })
      if (res.success) {
        toast.success(res.message || t('Connection succeeded'))
      }
    } finally {
      setTesting(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEdit ? t('Edit Data Source') : t('Add Data Source')}
      contentClassName='max-w-lg'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={handleTest}
            disabled={testing}
          >
            {testing ? t('Testing...') : t('Test connection')}
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
            placeholder='vm-primary'
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Base URL')} *</Label>
          <Input
            placeholder='http://victoria-metrics:8428'
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
          />
        </div>
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='grid gap-1.5'>
            <Label>{t('Username')}</Label>
            <Input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label>{t('Password')}</Label>
            <Input
              type='password'
              autoComplete='new-password'
              placeholder={
                isEdit && props.datasource?.password_set
                  ? t('Leave blank to keep the stored password')
                  : ''
              }
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Timeout (seconds)')}</Label>
          <Input
            type='number'
            min={1}
            value={timeoutSeconds}
            onChange={(e) => setTimeoutSeconds(Number(e.target.value))}
          />
        </div>
        <div className='flex items-center gap-2'>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
          <Label>{t('Enabled')}</Label>
        </div>
      </div>
    </Dialog>
  )
}
