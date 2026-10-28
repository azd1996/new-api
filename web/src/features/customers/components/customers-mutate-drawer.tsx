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
import { zodResolver } from '@hookform/resolvers/zod'
import { Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import { createCustomer, getCustomer, updateCustomer } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import {
  CUSTOMER_FORM_DEFAULT_VALUES,
  type CustomerFormValues,
  getCustomerFormSchema,
  transformCustomerToFormDefaults,
  transformFormDataToPayload,
} from '../lib'
import type { Customer } from '../types'
import { useCustomers } from './customers-provider'

type CustomersMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Customer
}

export function CustomersMutateDrawer(props: CustomersMutateDrawerProps) {
  const { t } = useTranslation()
  const isUpdate = !!props.currentRow
  const { triggerRefresh } = useCustomers()
  const [isSubmitting, setIsSubmitting] = useState(false)

  const form = useForm<CustomerFormValues>({
    resolver: zodResolver(getCustomerFormSchema(t)),
    defaultValues: CUSTOMER_FORM_DEFAULT_VALUES,
  })

  const prefixDiscounts = useFieldArray({
    control: form.control,
    name: 'prefix_discounts',
  })

  useEffect(() => {
    if (props.open && isUpdate && props.currentRow) {
      void getCustomer(props.currentRow.id).then((result) => {
        if (result.success && result.data) {
          form.reset(transformCustomerToFormDefaults(result.data))
        }
      })
    } else if (props.open && !isUpdate) {
      form.reset(CUSTOMER_FORM_DEFAULT_VALUES)
    }
  }, [props.open, isUpdate, props.currentRow, form])

  const onSubmit = async (data: CustomerFormValues) => {
    setIsSubmitting(true)
    try {
      const payload = transformFormDataToPayload(data)
      const result =
        isUpdate && props.currentRow
          ? await updateCustomer({ ...payload, id: props.currentRow.id })
          : await createCustomer(payload)

      if (result.success) {
        toast.success(
          t(
            isUpdate
              ? SUCCESS_MESSAGES.CUSTOMER_UPDATED
              : SUCCESS_MESSAGES.CUSTOMER_CREATED
          )
        )
        props.onOpenChange(false)
        triggerRefresh()
      } else {
        toast.error(
          result.message ||
            t(
              isUpdate
                ? ERROR_MESSAGES.UPDATE_FAILED
                : ERROR_MESSAGES.CREATE_FAILED
            )
        )
      }
    } finally {
      setIsSubmitting(false)
    }
  }
  return (
    <Sheet
      open={props.open}
      onOpenChange={(v) => {
        props.onOpenChange(v)
        if (!v) form.reset()
      }}
    >
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[600px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isUpdate ? t('Update Customer') : t('Add Customer')}
          </SheetTitle>
          <SheetDescription>
            {t(
              'Configure the customer name and per-model stat discounts. Discounts are multipliers (e.g. 0.8 = 80%) and only affect reporting, not actual billing.'
            )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='customer-form'
            onSubmit={(event) => void form.handleSubmit(onSubmit)(event)}
            className={sideDrawerFormClassName()}
          >
            <SideDrawerSection>
              <FormField
                control={form.control}
                name='customer_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Customer ID')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder={t('Enter customer ID')} />
                    </FormControl>
                    <FormDescription>
                      {t('Unique identifier (the downstream api_key_id)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Customer Name')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder={t('Enter a name')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormItem>
                <FormLabel>{t('Series Discounts')}</FormLabel>
                <FormDescription>
                  {t(
                    'Per-series discounts by model-name prefix (e.g. claude / gpt / gemini / grok); the longest matching prefix wins.'
                  )}
                </FormDescription>
                <div className='space-y-2'>
                  {prefixDiscounts.fields.map((row, index) => (
                    <div key={row.id} className='flex items-start gap-2'>
                      <FormField
                        control={form.control}
                        name={`prefix_discounts.${index}.prefix`}
                        render={({ field }) => (
                          <FormItem className='flex-1'>
                            <FormControl>
                              <Input
                                {...field}
                                placeholder={t(
                                  'Model name prefix (e.g. claude)'
                                )}
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <FormField
                        control={form.control}
                        name={`prefix_discounts.${index}.discount`}
                        render={({ field }) => (
                          <FormItem className='w-28'>
                            <FormControl>
                              <Input
                                {...field}
                                type='number'
                                step={0.01}
                                min={0}
                                placeholder={t('Discount')}
                                onChange={(e) =>
                                  field.onChange(
                                    Number.parseFloat(e.target.value) || 0
                                  )
                                }
                              />
                            </FormControl>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon'
                        onClick={() => prefixDiscounts.remove(index)}
                        aria-label={t('Remove')}
                      >
                        <Trash2 className='text-destructive h-4 w-4' />
                      </Button>
                    </div>
                  ))}
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      prefixDiscounts.append({ prefix: '', discount: 1 })
                    }
                  >
                    <Plus className='h-4 w-4' />
                    {t('Add series discount')}
                  </Button>
                </div>
              </FormItem>

              <FormField
                control={form.control}
                name='remark'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Remark')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder={t('Optional note')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button form='customer-form' type='submit' disabled={isSubmitting}>
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
