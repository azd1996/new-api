import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Row } from '@tanstack/react-table'
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { Channel } from '../../types'
import { ChannelsProvider, useChannels } from '../channels-provider'
import { DataTableRowActions } from '../data-table-row-actions'
import { CopyChannelDialog } from '../dialogs/copy-channel-dialog'

function Fixture(props: { channel: Channel }) {
  const context = useChannels()
  return (
    <>
      <DataTableRowActions row={{ original: props.channel } as Row<Channel>} />
      {context.open === 'copy-channel' && (
        <CopyChannelDialog open onOpenChange={() => context.setOpen(null)} />
      )}
    </>
  )
}

function mount(name = 'behind-cdnaigw_dept', tag = 'behind-cdnaigw') {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <Fixture
          channel={
            {
              id: 7,
              name,
              tag,
              type: 1,
              status: 1,
              channel_info: {},
            } as Channel
          }
        />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  return client
}

beforeEach(() => {
  localStorage.clear()
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'admin',
    display_name: 'Admin',
    role: 100,
    group: 'default',
  })
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { id: 8 } },
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
  localStorage.clear()
})

it('opens the shared dialog by keyboard and submits the default non-standard name without its prefix', async () => {
  mount()
  const user = userEvent.setup()
  screen.getByRole('button', { name: 'Copy Channel' }).focus()
  await user.keyboard('{Enter}')
  const dialog = within(await screen.findByRole('dialog'))
  expect(dialog.getByRole('textbox', { name: 'Channel Name' })).toHaveValue(
    'dept-非标'
  )
  expect(dialog.getByText(/Only the channel is copied/)).toBeVisible()
  await user.click(dialog.getByRole('button', { name: 'Copy Channel' }))
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/channel/copy_behind_cdnaigw/7',
      { name: 'dept-非标', reset_balance: true },
      expect.any(Object)
    )
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
})

it('allows the 57-byte pure copy but validates other names by UTF-8 bytes and keeps dedicated copying', async () => {
  mount(`behind-cdnaigw_${'a'.repeat(35)}`)
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Copy Channel' }))
  const dialog = within(await screen.findByRole('dialog'))
  const input = dialog.getByRole('textbox', { name: 'Channel Name' })
  const submit = dialog.getByRole('button', { name: 'Copy Channel' })
  expect(submit).toBeEnabled()
  await user.click(submit)
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/channel/copy_behind_cdnaigw/7',
      { name: `${'a'.repeat(35)}-非标`, reset_balance: true },
      expect.any(Object)
    )
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await user.click(screen.getByRole('button', { name: 'Copy Channel' }))
  const reopened = within(await screen.findByRole('dialog'))
  const renamed = reopened.getByRole('textbox', { name: 'Channel Name' })
  fireEvent.change(renamed, { target: { value: '中'.repeat(13) } })
  expect(reopened.getByRole('button', { name: 'Copy Channel' })).toBeDisabled()
  expect(renamed).toHaveAttribute('aria-invalid', 'true')
  expect(reopened.getByRole('alert')).toHaveTextContent('50 UTF-8 bytes')
  fireEvent.change(renamed, { target: { value: '中'.repeat(11) } })
  expect(
    reopened.getByText(/Other names use the dedicated copy flow/)
  ).toBeVisible()
  await user.click(reopened.getByRole('button', { name: 'Copy Channel' }))
  await waitFor(() =>
    expect(api.post).toHaveBeenLastCalledWith(
      '/api/channel/copy_behind_cdnaigw/7',
      { name: '中'.repeat(11), reset_balance: true },
      expect.any(Object)
    )
  )
  expect(input).not.toBeInTheDocument()
})

it('keeps failed copies open and allows retry without losing the target name', async () => {
  vi.mocked(api.post).mockResolvedValueOnce({
    data: { success: false, message: 'Duplicate channel name' },
  })
  mount()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Copy Channel' }))
  const dialog = within(await screen.findByRole('dialog'))
  await user.click(dialog.getByRole('button', { name: 'Copy Channel' }))
  await waitFor(() =>
    expect(dialog.getByRole('button', { name: 'Copy Channel' })).toBeEnabled()
  )
  expect(dialog.getByRole('textbox', { name: 'Channel Name' })).toHaveValue(
    'dept-非标'
  )
  await user.click(dialog.getByRole('button', { name: 'Copy Channel' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
})

it('does not offer another non-standard shortcut for non-standard channels but retains dedicated menu copying', async () => {
  mount('behind-cdnaigw_dept-非标')
  const user = userEvent.setup()
  expect(
    screen.queryByRole('button', { name: 'Copy Channel' })
  ).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  await user.click(
    await screen.findByRole('menuitem', { name: 'Copy Channel' })
  )
  const dialog = within(await screen.findByRole('dialog'))
  expect(dialog.getByRole('textbox', { name: 'Channel Name' })).toHaveValue('')
  expect(dialog.getByRole('button', { name: 'Copy Channel' })).toBeDisabled()
})

it('retains ordinary suffix copying for channels without the behind tag', async () => {
  mount('ordinary', '')
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  await user.click(
    await screen.findByRole('menuitem', { name: 'Copy Channel' })
  )
  const dialog = within(await screen.findByRole('dialog'))
  expect(dialog.getByRole('textbox', { name: 'Name Suffix' })).toHaveValue(
    '_copy'
  )
  await user.click(dialog.getByRole('button', { name: 'Copy Channel' }))
  await waitFor(() => expect(api.post).toHaveBeenCalled())
})
