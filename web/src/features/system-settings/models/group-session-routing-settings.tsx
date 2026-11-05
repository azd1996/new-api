import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

import {
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { useSystemOptions } from '../hooks/use-system-options'
import { useUpdateOption } from '../hooks/use-update-option'

const routingKey = 'session_routing_setting.group_enabled'
const groupEnabledSchema = z.record(z.string(), z.boolean())

export function GroupSessionRoutingSettings() {
  const { t } = useTranslation()
  const options = useSystemOptions()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()

  if (options.isError) {
    return <ErrorState onRetry={() => void options.refetch()} />
  }
  if (!options.data) return <LoadingState />

  let groupEnabled: Record<string, boolean>
  let groups: string[]
  try {
    const routingValue = options.data.data.find(
      (option) => option.key === routingKey
    )?.value
    groupEnabled = groupEnabledSchema.parse(JSON.parse(routingValue ?? '{}'))
    const groupValue = options.data.data.find(
      (option) => option.key === 'GroupRatio'
    )?.value
    const actualGroups = z
      .record(z.string(), z.number())
      .parse(JSON.parse(groupValue || '{}'))
    groups = Object.keys(actualGroups).filter((name) =>
      name.startsWith('behind-cdnaigw_')
    )
  } catch {
    return (
      <ErrorState
        description={t(
          'Invalid group session routing configuration. Reload settings before editing.'
        )}
        onRetry={() => void options.refetch()}
      />
    )
  }

  if (groups.length === 0) return null

  return (
    <section
      className='mt-6 space-y-3'
      aria-label={t('Automatic session routing')}
    >
      <h3 className='text-sm font-semibold'>
        {t('Automatic session routing')}
      </h3>
      <p
        id='session-routing-description'
        className='text-muted-foreground text-sm'
      >
        {t(
          'Enabled by default for behind-cdnaigw_ groups. Routing applies only when the current model has a non-disabled channel whose name ends in -非标; otherwise requests use standard channels. Turning this off uses standard channels without disabling any channel. Changes are saved immediately.'
        )}
      </p>
      {groups.map((group, index) => (
        <SettingsSwitchItem key={group}>
          <SettingsSwitchContent>
            <Label htmlFor={`session-routing-${index}`} className='break-all'>
              {group}
            </Label>
          </SettingsSwitchContent>
          <Switch
            id={`session-routing-${index}`}
            aria-describedby='session-routing-description'
            checked={groupEnabled[group] !== false}
            disabled={updateOption.isPending || options.isFetching}
            onCheckedChange={(enabled) => {
              updateOption.mutate(
                {
                  key: routingKey,
                  value: JSON.stringify({ ...groupEnabled, [group]: enabled }),
                },
                {
                  onSuccess: () => {
                    // Keep the successful value visible while the options query refreshes.
                    queryClient.setQueryData<typeof options.data>(
                      ['system-options'],
                      (previous) => {
                        if (!previous) return previous
                        return {
                          ...previous,
                          data: [
                            ...previous.data.filter(
                              (option) => option.key !== routingKey
                            ),
                            {
                              key: routingKey,
                              value: JSON.stringify({
                                ...groupEnabled,
                                [group]: enabled,
                              }),
                            },
                          ],
                        }
                      }
                    )
                  },
                }
              )
            }}
          />
        </SettingsSwitchItem>
      ))}
    </section>
  )
}
