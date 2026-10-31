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
import { ExternalLink, Pencil } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import dayjs from '@/lib/dayjs'

import type { ErrorStatItem, ErrorStatsGroupBy } from '../types'

// Which editable alias field a save originated from, used to pick the right
// toast and preserve the sibling fields' values.
type AliasField = 'short_name' | 'attribution'

// The full set of editable alias fields, including the free-text note.
type AliasSaveField = AliasField | 'note'

// Merged alias values plus the field that triggered the save.
export interface AliasSaveInput {
  short_name: string
  attribution: string
  note: string
  field: AliasSaveField
}

function formatSeconds(seconds: number): string {
  if (!seconds) return '-'
  return dayjs(seconds * 1000).format('YYYY-MM-DD HH:mm:ss')
}

function formatPercentage(count: number, total: number): string {
  if (!total) return '0%'
  return `${((count / total) * 100).toFixed(1)}%`
}

interface AliasEditCellProps {
  item: ErrorStatItem
  field: AliasField
  options: string[]
  isSaving: boolean
  onSave: (item: ErrorStatItem, next: AliasSaveInput) => void
}

// Editable cell for a single alias field (short_name or attribution) shown in
// signature mode. Saving always sends BOTH current values merged so editing one
// field never drops the other.
function AliasEditCell(props: AliasEditCellProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const current = props.item[props.field]
  const [draft, setDraft] = useState(current)

  useEffect(() => {
    if (open) setDraft(current)
  }, [open, current])

  const hasValue = current.trim() !== ''
  const isShortName = props.field === 'short_name'
  const emptyLabel = isShortName ? t('Uncategorized') : t('Unattributed')
  const fieldLabel = isShortName ? t('Short name') : t('Attribution')
  let editLabel: string
  if (isShortName) {
    editLabel = hasValue ? t('Edit short name') : t('Set short name')
  } else {
    editLabel = hasValue ? t('Edit attribution') : t('Set attribution')
  }
  const placeholder = isShortName
    ? t('Select or enter a short name')
    : t('Select or enter an attribution')

  const buildPayload = (value: string): AliasSaveInput => ({
    short_name: isShortName ? value : props.item.short_name,
    attribution: isShortName ? props.item.attribution : value,
    note: props.item.note,
    field: props.field,
  })

  const handleSave = () => {
    props.onSave(props.item, buildPayload(draft.trim()))
    setOpen(false)
  }

  return (
    <div className='flex items-center gap-1.5'>
      {hasValue ? (
        <Badge variant='secondary' className='max-w-[160px] truncate'>
          {current}
        </Badge>
      ) : (
        <span className='text-muted-foreground text-xs'>{emptyLabel}</span>
      )}

      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              size='icon-xs'
              aria-label={editLabel}
            />
          }
        >
          <Pencil className='size-3' aria-hidden='true' />
        </PopoverTrigger>
        <PopoverContent align='start' className='w-[280px] space-y-2 p-3'>
          <Label className='text-muted-foreground text-xs'>{fieldLabel}</Label>
          <Combobox
            options={props.options.map((value) => ({ value, label: value }))}
            value={draft}
            onValueChange={(value) => setDraft(value ?? '')}
            allowCustomValue
            openOnFocus
            placeholder={placeholder}
            searchPlaceholder={placeholder}
            emptyText={t('No option found.')}
            className='h-8'
          />
          <div className='flex justify-end gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              size='sm'
              onClick={handleSave}
              disabled={props.isSaving}
            >
              {t('Save')}
            </Button>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  )
}

interface NoteEditCellProps {
  item: ErrorStatItem
  isSaving: boolean
  onSave: (item: ErrorStatItem, next: AliasSaveInput) => void
}

// Editable free-text note cell shown in signature mode. The note explains WHY
// the admin categorized a signature a given way. Saving always sends the row's
// current short_name/attribution merged so editing the note never drops them.
function NoteEditCell(props: NoteEditCellProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const current = props.item.note
  const [draft, setDraft] = useState(current)

  useEffect(() => {
    if (open) setDraft(current)
  }, [open, current])

  const hasValue = current.trim() !== ''
  const editLabel = hasValue ? t('Edit note') : t('Add note')

  const buildPayload = (value: string): AliasSaveInput => ({
    short_name: props.item.short_name,
    attribution: props.item.attribution,
    note: value,
    field: 'note',
  })

  const handleSave = () => {
    props.onSave(props.item, buildPayload(draft.trim()))
    setOpen(false)
  }

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter') {
      event.preventDefault()
      handleSave()
    }
  }

  return (
    <div className='flex items-center gap-1.5'>
      {hasValue ? (
        <Tooltip>
          <TooltipTrigger
            render={<span className='block max-w-[200px] truncate text-xs' />}
          >
            {current}
          </TooltipTrigger>
          <TooltipContent className='max-w-[360px] break-words'>
            {current}
          </TooltipContent>
        </Tooltip>
      ) : (
        <span className='text-muted-foreground text-xs'>{t('Add note')}</span>
      )}

      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              size='icon-xs'
              aria-label={editLabel}
            />
          }
        >
          <Pencil className='size-3' aria-hidden='true' />
        </PopoverTrigger>
        <PopoverContent align='start' className='w-[280px] space-y-2 p-3'>
          <Label className='text-muted-foreground text-xs'>
            {t('Explanation')}
          </Label>
          <Input
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={handleKeyDown}
            disabled={props.isSaving}
            className='h-8'
          />
          <div className='flex justify-end gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              size='sm'
              onClick={handleSave}
              disabled={props.isSaving}
            >
              {t('Save')}
            </Button>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  )
}

function CountCell(props: { count: number; total: number }) {
  return (
    <div className='flex flex-col'>
      <span className='font-medium tabular-nums'>{props.count}</span>
      <span className='text-muted-foreground text-xs tabular-nums'>
        {formatPercentage(props.count, props.total)}
      </span>
    </div>
  )
}

function SeenCell(props: { firstSeen: number; lastSeen: number }) {
  return (
    <div className='flex flex-col text-xs tabular-nums'>
      <span>{formatSeconds(props.firstSeen)}</span>
      <span className='text-muted-foreground'>
        {formatSeconds(props.lastSeen)}
      </span>
    </div>
  )
}

function ModelsCell(props: { item: ErrorStatItem }) {
  return (
    <div className='flex flex-wrap gap-1'>
      {props.item.models.length === 0 && (
        <span className='text-muted-foreground text-xs'>-</span>
      )}
      {props.item.models.map((m) => (
        <Badge key={m.model} variant='outline' className='max-w-[180px]'>
          <span className='truncate'>{m.model}</span>
          <span className='text-muted-foreground'>×{m.count}</span>
        </Badge>
      ))}
    </div>
  )
}

function ChannelsCell(props: { item: ErrorStatItem }) {
  return (
    <div className='flex flex-wrap gap-1'>
      {props.item.channels.length === 0 && (
        <span className='text-muted-foreground text-xs'>-</span>
      )}
      {props.item.channels.map((c) => {
        const name = c.channel_name || `#${c.channel_id}`
        return (
          <Tooltip key={c.channel_id}>
            <TooltipTrigger
              render={<Badge variant='outline' className='max-w-[450px]' />}
            >
              <span className='truncate'>{name}</span>
              <span className='text-muted-foreground'>×{c.count}</span>
            </TooltipTrigger>
            <TooltipContent className='max-w-[480px] break-words'>
              {name} ×{c.count}
            </TooltipContent>
          </Tooltip>
        )
      })}
    </div>
  )
}

function CustomersCell(props: { item: ErrorStatItem }) {
  return (
    <div className='flex flex-wrap gap-1'>
      {props.item.customers.length === 0 && (
        <span className='text-muted-foreground text-xs'>-</span>
      )}
      {props.item.customers.map((c) => {
        // 展示格式：客户ID(客户名)；没有客户名时只展示客户ID。
        const label = c.customer_name
          ? `${c.customer_id}(${c.customer_name})`
          : c.customer_id
        return (
          <Tooltip key={c.customer_id}>
            <TooltipTrigger
              render={<Badge variant='outline' className='max-w-[260px]' />}
            >
              <span className='truncate'>{label}</span>
              <span className='text-muted-foreground'>×{c.count}</span>
            </TooltipTrigger>
            <TooltipContent className='max-w-[360px] break-words'>
              {label} ×{c.count}
            </TooltipContent>
          </Tooltip>
        )
      })}
    </div>
  )
}

// GROUP_LABEL_PLACEHOLDER
interface ErrorStatsTableProps {
  items: ErrorStatItem[]
  total: number
  isLoading: boolean
  viewMode: ErrorStatsGroupBy
  savingHash: string | null
  shortNameOptions: string[]
  attributionOptions: string[]
  onSaveAlias: (item: ErrorStatItem, next: AliasSaveInput) => void
  onDrillDown: (item: ErrorStatItem) => void
}

export function ErrorStatsTable(props: ErrorStatsTableProps) {
  const { t } = useTranslation()

  const isSignatureMode = props.viewMode === 'signature'
  const colSpan = isSignatureMode ? 11 : 6
  const groupHeader =
    props.viewMode === 'attribution'
      ? t('Error Attribution')
      : t('Detailed Error Type')
  const groupEmptyLabel =
    props.viewMode === 'attribution' ? t('Unattributed') : t('Uncategorized')

  return (
    <div className='h-full overflow-auto rounded-lg border'>
      <Table>
        <TableHeader className='bg-muted/50 sticky top-0 z-10'>
          <TableRow>
            {isSignatureMode ? (
              <>
                <TableHead className='min-w-[160px]'>
                  {t('Detailed Error Type')}
                </TableHead>
                <TableHead className='min-w-[160px]'>
                  {t('Error Attribution')}
                </TableHead>
                <TableHead className='min-w-[160px]'>
                  {t('Explanation')}
                </TableHead>
                <TableHead className='min-w-[280px]'>
                  {t('Detailed Error')}
                </TableHead>
              </>
            ) : (
              <TableHead className='min-w-[200px]'>{groupHeader}</TableHead>
            )}
            <TableHead className='min-w-[120px]'>{t('Occurrences')}</TableHead>
            <TableHead className='min-w-[180px]'>{t('First Seen')}</TableHead>
            {isSignatureMode && (
              <TableHead className='min-w-[100px]'>
                {t('Status Code')}
              </TableHead>
            )}
            <TableHead className='min-w-[180px]'>{t('Models')}</TableHead>
            <TableHead className='min-w-[450px]'>{t('Channels')}</TableHead>
            <TableHead className='min-w-[200px]'>{t('Customers')}</TableHead>
            {isSignatureMode && (
              <TableHead className='min-w-[120px] text-right'>
                {t('Actions')}
              </TableHead>
            )}
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.isLoading && (
            <TableRow>
              <TableCell colSpan={colSpan} className='h-24 text-center'>
                {t('Loading...')}
              </TableCell>
            </TableRow>
          )}

          {!props.isLoading && props.items.length === 0 && (
            <TableRow>
              <TableCell
                colSpan={colSpan}
                className='text-muted-foreground h-24 text-center'
              >
                {t('No error stats')}
              </TableCell>
            </TableRow>
          )}

          {!props.isLoading &&
            isSignatureMode &&
            props.items.map((item) => (
              <TableRow key={item.signature_hash}>
                <TableCell>
                  <AliasEditCell
                    item={item}
                    field='short_name'
                    options={props.shortNameOptions}
                    isSaving={props.savingHash === item.signature_hash}
                    onSave={props.onSaveAlias}
                  />
                </TableCell>
                <TableCell>
                  <AliasEditCell
                    item={item}
                    field='attribution'
                    options={props.attributionOptions}
                    isSaving={props.savingHash === item.signature_hash}
                    onSave={props.onSaveAlias}
                  />
                </TableCell>
                <TableCell>
                  <NoteEditCell
                    item={item}
                    isSaving={props.savingHash === item.signature_hash}
                    onSave={props.onSaveAlias}
                  />
                </TableCell>
                <TableCell>
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <span className='block max-w-[360px] truncate font-mono text-xs' />
                      }
                    >
                      {item.signature}
                    </TooltipTrigger>
                    <TooltipContent className='max-w-[480px] break-words'>
                      {item.signature}
                    </TooltipContent>
                  </Tooltip>
                </TableCell>
                <TableCell>
                  <CountCell count={item.count} total={props.total} />
                </TableCell>
                <TableCell>
                  <SeenCell
                    firstSeen={item.first_seen}
                    lastSeen={item.last_seen}
                  />
                </TableCell>
                <TableCell className='tabular-nums'>
                  {item.status_code || '-'}
                </TableCell>
                <TableCell>
                  <ModelsCell item={item} />
                </TableCell>
                <TableCell>
                  <ChannelsCell item={item} />
                </TableCell>
                <TableCell>
                  <CustomersCell item={item} />
                </TableCell>
                <TableCell className='text-right'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => props.onDrillDown(item)}
                  >
                    <ExternalLink className='size-3.5' aria-hidden='true' />
                    {t('View logs')}
                  </Button>
                </TableCell>
              </TableRow>
            ))}

          {!props.isLoading &&
            !isSignatureMode &&
            props.items.map((item) => (
              <TableRow key={item.group_key}>
                <TableCell>
                  {item.group_label.trim() !== '' ? (
                    <Badge
                      variant='secondary'
                      className='max-w-[200px] truncate'
                    >
                      {item.group_label}
                    </Badge>
                  ) : (
                    <span className='text-muted-foreground text-xs'>
                      {groupEmptyLabel}
                    </span>
                  )}
                </TableCell>
                <TableCell>
                  <CountCell count={item.count} total={props.total} />
                </TableCell>
                <TableCell>
                  <SeenCell
                    firstSeen={item.first_seen}
                    lastSeen={item.last_seen}
                  />
                </TableCell>
                <TableCell>
                  <ModelsCell item={item} />
                </TableCell>
                <TableCell>
                  <ChannelsCell item={item} />
                </TableCell>
                <TableCell>
                  <CustomersCell item={item} />
                </TableCell>
              </TableRow>
            ))}
        </TableBody>
      </Table>
    </div>
  )
}



