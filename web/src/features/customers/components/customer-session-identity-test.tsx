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
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import {
  testCustomerSessionIdentity,
  type SessionIdentityTestResponse,
} from '../api'
import { splitJsonBodies } from '../lib/session-identity'
import type { SessionIdentity } from '../types'

type CustomerSessionIdentityTestProps = {
  config: SessionIdentity | null
  customerId?: string
}

export function CustomerSessionIdentityTest(
  props: CustomerSessionIdentityTestProps
) {
  const { t } = useTranslation()
  const [requestsText, setRequestsText] = useState('')
  const [expectSame, setExpectSame] = useState(true)
  const [result, setResult] = useState<SessionIdentityTestResponse | null>(null)

  const testMutation = useMutation({
    mutationFn: testCustomerSessionIdentity,
    onSuccess: (data) => setResult(data),
    onError: (error: unknown) =>
      handleServerError(error, t('Failed to test session identity extraction')),
  })

  const canTest = props.config !== null || !!props.customerId

  const handleTest = () => {
    const bodies = splitJsonBodies(requestsText)
    if (bodies.length === 0) {
      toast.error(t('Paste at least one valid JSON request body'))
      return
    }
    setResult(null)
    if (props.config) {
      // Test the rules as configured; the enabled switch only gates production
      // traffic, not the dry run.
      testMutation.mutate({
        config: { ...props.config, enabled: true },
        items: bodies.map((body) => ({ body })),
        expect_same: expectSame,
      })
      return
    }
    testMutation.mutate({
      customer_id: props.customerId,
      items: bodies.map((body) => ({ body })),
      expect_same: expectSame,
    })
  }

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Session identity test')}
        description={t(
          'Paste one or more complete request bodies to check which session ID the gateway would extract. Nothing is persisted.'
        )}
      />
      {!canTest && (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Customize session identity first, or open an existing customer to test its saved configuration.'
          )}
        </p>
      )}
      {canTest && (
        <>
          <Textarea
            value={requestsText}
            onChange={(event) => setRequestsText(event.target.value)}
            rows={5}
            placeholder='{"model":"claude-opus-4-6","system":[...],"messages":[...]}'
            aria-label={t('Request bodies')}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Paste minified JSON one per line or pretty-printed objects back to back.'
            )}
          </p>
          <div className='flex flex-wrap items-center gap-3'>
            <Button
              type='button'
              variant='outline'
              onClick={handleTest}
              disabled={testMutation.isPending}
            >
              {testMutation.isPending ? t('Testing...') : t('Test extraction')}
            </Button>
            <div className='flex items-center gap-2'>
              <Checkbox
                id='session-identity-expect-same'
                checked={expectSame}
                onCheckedChange={(checked) => setExpectSame(checked === true)}
              />
              <Label
                htmlFor='session-identity-expect-same'
                className='text-sm font-normal'
              >
                {t('Expect the same session ID for all requests')}
              </Label>
            </div>
          </div>
          {result && (
            <div className='space-y-2'>
              <div className='flex flex-wrap items-center gap-2'>
                <Badge variant={result.summary.stable ? 'default' : 'warning'}>
                  {result.summary.stable
                    ? t('Stable')
                    : t('Unstable ({{count}} session IDs)', {
                        count: result.summary.distinct_session_ids,
                      })}
                </Badge>
                <span className='text-muted-foreground text-xs'>
                  {t('{{resolved}} of {{total}} requests resolved', {
                    resolved: result.summary.ok_count,
                    total: result.summary.count,
                  })}
                </span>
              </div>
              <div className='rounded-md border'>
                <div className='text-muted-foreground grid grid-cols-[3rem_1fr_10rem] gap-2 border-b px-3 py-2 text-xs font-medium'>
                  <span>#</span>
                  <span>{t('Session ID')}</span>
                  <span>{t('Source')}</span>
                </div>
                <div className='max-h-56 overflow-y-auto'>
                  {result.results.map((item) => (
                    <div
                      key={item.index}
                      className='grid grid-cols-[3rem_1fr_10rem] items-center gap-2 border-b px-3 py-2 text-xs last:border-b-0'
                    >
                      <span className='text-muted-foreground'>
                        {item.index + 1}
                      </span>
                      <span
                        className='truncate font-mono'
                        title={item.session_id || item.error}
                      >
                        {item.ok
                          ? item.session_id || t('(client-managed)')
                          : item.error}
                      </span>
                      <span className='text-muted-foreground truncate'>
                        {item.source}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}
        </>
      )}
    </SideDrawerSection>
  )
}
