import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Row } from '@tanstack/react-table'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import type { LogAnalysisResult } from '@/features/usage-logs/types'
import { api } from '@/lib/api'

import { getCustomerLogAnalysis } from '../../api'
import type { Customer } from '../../types'
import { CustomerStatsDialog } from '../customer-stats-dialog'
import { CustomersDialogs } from '../customers-dialogs'
import { CustomersProvider } from '../customers-provider'
import { DataTableRowActions } from '../data-table-row-actions'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const customer: Customer = {
  id: 42,
  customer_id: 'external-key-123',
  name: 'Test customer',
  remark: '',
  prefix_discounts: {},
  created_time: 0,
  updated_time: 0,
}
const data: LogAnalysisResult = {
  channels: [],
  bucket_labels: ['0-1K'],
  latency: {
    mean: {
      buckets: [
        { bucket: '0-1K', total: { lat: 250, frt: 100, cnt: 3 }, channels: [] },
      ],
    },
    p50: { buckets: [] },
    p90: { buckets: [] },
    p99: { buckets: [] },
  },
  success: {
    total: { total: 4, success: 3, error: 2, retry_override: 1, rate: 0.75 },
    channels: [],
  },
  cache: {
    total: {
      gross_rate: 0.5,
      eligible_rate: 0.625,
      cache_read: 500,
      input_all: 1000,
      input_eligible: 800,
      hit: 1,
      cold_write: 1,
      too_small: 1,
      other_miss: 0,
    },
    channels: [],
  },
  scanned: 3,
  truncated: true,
  thresholds: [],
  threshold_default: 1024,
}

function renderStats() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <CustomerStatsDialog customer={customer} onClose={vi.fn()} />
    </QueryClientProvider>
  )
}

it('statistics icon opens customer analysis and sends the business customer ID to both APIs', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url === '/api/log/analysis' ? data : { enabled: false },
    },
  }))
  const user = userEvent.setup()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <CustomersProvider>
        <DataTableRowActions row={{ original: customer } as Row<Customer>} />
        <CustomersDialogs />
      </CustomersProvider>
    </QueryClientProvider>
  )
  expect(get).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Customer statistics' }))
  expect(
    await screen.findByRole('dialog', { name: 'Customer statistics' })
  ).toBeVisible()
  expect(await screen.findByText('75%')).toBeVisible()
  expect(screen.getByText('50%')).toBeVisible()
  expect(screen.getByText('62.5%')).toBeVisible()
  expect(
    screen.getByRole('table', { name: 'Latency / FRT (ms)' })
  ).toHaveTextContent('250 / 100 / 3')
  expect(
    screen.getByText('Cache analysis is unavailable on this deployment')
  ).toBeVisible()
  expect(screen.getByRole('status')).toHaveTextContent('partial sample')
  expect(get.mock.calls.map(([url]) => url).sort()).toEqual([
    '/api/log/analysis',
    '/api/log/cache-skeleton/sessions/analysis',
  ])
  for (const [, config] of get.mock.calls) {
    expect(config?.params).toMatchObject({
      customer_id: 'external-key-123',
      start_timestamp: expect.any(Number),
      end_timestamp: expect.any(Number),
    })
    expect(config?.signal).toBeDefined()
  }
})

it('cache analysis failure leaves request statistics visible and can be retried', async () => {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data:
      url === '/api/log/analysis'
        ? { success: true, data }
        : { success: false, message: 'Cache service unavailable' },
  }))
  renderStats()
  expect(await screen.findByText('75%')).toBeVisible()
  expect(await screen.findByText('Cache service unavailable')).toBeVisible()
  const cacheRegion = screen.getByRole('region', {
    name: 'Cache loss breakdown',
  })
  get.mockImplementation(async () => ({
    data: {
      success: true,
      data: { enabled: true, loss_summary: { analyzed_turns: 0, items: [] } },
    },
  }))
  await userEvent.setup().click(within(cacheRegion).getByRole('button'))
  expect(
    await screen.findByText('No analyzable Claude cache attempts')
  ).toBeVisible()
})

it('empty analysis displays no data and missing rates as dashes', async () => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/log/analysis'
          ? {
              ...data,
              truncated: false,
              success: {
                ...data.success,
                total: { ...data.success.total, total: 0, rate: 0 },
              },
            }
          : { enabled: true, loss_summary: { analyzed_turns: 0 } },
    },
  }))
  renderStats()
  expect(await screen.findByText('No data')).toBeVisible()
  expect(screen.getAllByText('—').length).toBeGreaterThan(0)
})

it('analysis API rejects a business failure instead of displaying it as empty statistics', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: false, message: 'Analysis unavailable' },
  })
  await expect(
    getCustomerLogAnalysis({
      customer_id: 'customer',
      start_timestamp: 1,
      end_timestamp: 2,
    })
  ).rejects.toThrow('Analysis unavailable')
})
