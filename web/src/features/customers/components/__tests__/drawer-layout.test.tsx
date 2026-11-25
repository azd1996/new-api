import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { Customer } from '../../types'
import { CustomersMutateDrawer } from '../customers-mutate-drawer'
import { CustomersProvider } from '../customers-provider'

test('editing a customer increases the drawer width cap while keeping it within the viewport', async () => {
  const customer: Customer = {
    id: 1,
    customer_id: 'customer-a',
    name: 'Customer A',
    remark: '',
    prefix_discounts: {},
    created_time: 0,
    updated_time: 0,
  }
  const get = vi
    .spyOn(api, 'get')
    .mockImplementation(async (url) => ({
      data: { success: true, data: url === '/api/customer/1' ? customer : [] },
    }))
  const client = new QueryClient()

  try {
    render(
      <QueryClientProvider client={client}>
        <CustomersProvider>
          <CustomersMutateDrawer
            open
            onOpenChange={vi.fn()}
            currentRow={customer}
          />
        </CustomersProvider>
      </QueryClientProvider>
    )

    await screen.findByDisplayValue('Customer A')
    expect(screen.getByRole('dialog')).toHaveClass(
      'w-full',
      'sm:max-w-[1800px]'
    )
  } finally {
    client.clear()
    get.mockRestore()
  }
})
