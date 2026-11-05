import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  getOptionValue,
  useSystemOptions,
} from '../../hooks/use-system-options'
import { GroupSessionRoutingSettings } from '../group-session-routing-settings'
import { RatioSettingsCard } from '../ratio-settings-card'

const routingKey = 'session_routing_setting.group_enabled'

function RoutingWorkspace() {
  const options = useSystemOptions()
  const groups = getOptionValue(options.data?.data, {
    GroupRatio: '{}',
    TopupGroupRatio: '{}',
    UserUsableGroups: '{}',
    GroupGroupRatio: '{}',
    AutoGroups: '[]',
    MaxTokenAutoGroups: 5,
    DefaultUseAutoGroup: false,
    GroupSpecialUsableGroup: '{}',
  })
  if (!options.data) return null
  return (
    <RatioSettingsCard
      groupDefaults={groups}
      modelDefaults={{
        ModelPrice: '{}',
        ModelRatio: '{}',
        CacheRatio: '{}',
        CreateCacheRatio: '{}',
        CompletionRatio: '{}',
        ImageRatio: '{}',
        AudioRatio: '{}',
        AudioCompletionRatio: '{}',
        ExposeRatioEnabled: false,
        BillingMode: '{}',
        BillingExpr: '{}',
        PluginBillingExpr: '{}',
      }}
      toolPricesDefault='{}'
      visibleTabs={['groups']}
    />
  )
}

function mount(
  value?: string,
  groupRatio = '{"default":1,"behind-cdnaigw_dept":1,"behind-cdnaigw-other":1}',
  workspace = false
) {
  let options = [
    { key: 'GroupRatio', value: groupRatio },
    ...(value === undefined ? [] : [{ key: routingKey, value }]),
  ]
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data: options },
  }))
  vi.spyOn(api, 'put').mockImplementation(async (_url, body) => {
    const request = body as { key: string; value: string }
    options = [
      ...options.filter((option) => option.key !== request.key),
      request,
    ]
    return { data: { success: true } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      {workspace ? <RoutingWorkspace /> : <GroupSessionRoutingSettings />}
    </QueryClientProvider>
  )
  return client
}

afterEach(() => vi.restoreAllMocks())

it('preserves unsaved group ratio edits when a routing change refreshes options', async () => {
  mount('{}', '{"behind-cdnaigw_dept":1}', true)
  const toggle = await screen.findByRole('switch', {
    name: 'behind-cdnaigw_dept',
  })
  const table = screen.getByRole('table', { name: 'Pricing groups' })
  const ratio = within(table).getByRole('spinbutton', { name: 'Ratio' })
  fireEvent.change(ratio, { target: { value: '0.7' } })
  await waitFor(() => expect(toggle).toBeEnabled())
  await userEvent.setup().click(toggle)
  await waitFor(() => expect(toggle).not.toBeChecked())
  await waitFor(() => expect(toggle).toBeEnabled())
  expect(ratio).toHaveValue(0.7)
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: routingKey,
    value: '{"behind-cdnaigw_dept":false}',
  })
})

it('shows only actual prefixed groups, defaults to automatic, and saves an explicit false immediately', async () => {
  mount()
  const toggle = await screen.findByRole('switch', {
    name: 'behind-cdnaigw_dept',
  })
  expect(screen.getAllByRole('switch')).toHaveLength(1)
  expect(toggle).toBeChecked()
  expect(
    screen.getByText(/current model has a non-disabled channel/)
  ).toBeVisible()
  await waitFor(() => expect(toggle).toBeEnabled())
  toggle.focus()
  await userEvent.setup().keyboard(' ')
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: routingKey,
      value: '{"behind-cdnaigw_dept":false}',
    })
  )
  await waitFor(() => expect(toggle).not.toBeChecked())
})

it('loads false, re-enables automatic routing and preserves unrelated map entries', async () => {
  mount(
    '{"behind-cdnaigw_dept":false,"behind-cdnaigw_retired":false,"default":false}'
  )
  const toggle = await screen.findByRole('switch', {
    name: 'behind-cdnaigw_dept',
  })
  expect(toggle).not.toBeChecked()
  await waitFor(() => expect(toggle).toBeEnabled())
  await userEvent.setup().click(toggle)
  await waitFor(() => expect(toggle).toBeChecked())
  expect(api.put).toHaveBeenCalledWith('/api/option/', {
    key: routingKey,
    value:
      '{"behind-cdnaigw_dept":true,"behind-cdnaigw_retired":false,"default":false}',
  })
})

it('retains the saved switch value on business failure and allows retry', async () => {
  mount()
  vi.mocked(api.put).mockResolvedValueOnce({
    data: { success: false, message: 'Save rejected' },
  })
  const toggle = await screen.findByRole('switch', {
    name: 'behind-cdnaigw_dept',
  })
  await waitFor(() => expect(toggle).toBeEnabled())
  await userEvent.setup().click(toggle)
  await waitFor(() => expect(api.put).toHaveBeenCalled())
  await waitFor(() => expect(toggle).toBeEnabled())
  expect(toggle).toBeChecked()
  await userEvent.setup().click(toggle)
  await waitFor(() => expect(toggle).not.toBeChecked())
})

it.each(['null', '[]', '{"behind-cdnaigw_dept":"false"}', '{'])(
  'prevents overwriting malformed configuration %s',
  async (value) => {
    mount(value)
    expect(
      await screen.findByText(
        'Invalid group session routing configuration. Reload settings before editing.'
      )
    ).toBeVisible()
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
    expect(api.put).not.toHaveBeenCalled()
  }
)

it('hides the section when there are no eligible groups', async () => {
  const client = mount('{}', '{"default":1}')
  await waitFor(() =>
    expect(client.getQueryState(['system-options'])?.status).toBe('success')
  )
  expect(
    screen.queryByRole('region', { name: 'Automatic session routing' })
  ).not.toBeInTheDocument()
  expect(screen.queryByRole('switch')).not.toBeInTheDocument()
})
