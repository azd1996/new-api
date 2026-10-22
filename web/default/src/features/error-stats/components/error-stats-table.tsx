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
import { ExternalLink, Pencil, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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

import type { ErrorStatItem } from '../types'

function formatSeconds(seconds: number): string {
  if (!seconds) return '-'
  return dayjs(seconds * 1000).format('YYYY-MM-DD HH:mm:ss')
}

interface ShortNameCellProps {
  item: ErrorStatItem
  isSaving: boolean
  onSave: (shortName: string) => void
  onClear: () => void
}

function ShortNameCell(props: ShortNameCellProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState(props.item.short_name)

  useEffect(() => {
    if (open) setDraft(props.item.short_name)
  }, [open, props.item.short_name])

  const hasShortName = props.item.short_name.trim() !== ''

  const handleSave = () => {
    props.onSave(draft.trim())
    setOpen(false)
  }

  return (
    <div className='flex items-center gap-1.5'>
      {hasShortName ? (
        <Badge variant='secondary' className='max-w-[160px] truncate'>
          {props.item.short_name}
        </Badge>
      ) : (
        <span className='text-muted-foreground text-xs'>
          {t('Uncategorized')}
        </span>
      )}

      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              size='icon-xs'
              aria-label={
                hasShortName ? t('Edit short name') : t('Set short name')
              }
            />
          }
        >
          <Pencil className='size-3' aria-hidden='true' />
        </PopoverTrigger>
        <PopoverContent align='start' className='w-[280px] space-y-2 p-3'>
          <Label
            htmlFor={`short-name-${props.item.signature_hash}`}
            className='text-muted-foreground text-xs'
          >
            {t('Short name')}
          </Label>
          <Input
            id={`short-name-${props.item.signature_hash}`}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                handleSave()
              }
            }}
            className='h-8'
            autoFocus
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

      {hasShortName && (
        <Button
          type='button'
          variant='ghost'
          size='icon-xs'
          aria-label={t('Clear category')}
          disabled={props.isSaving}
          onClick={props.onClear}
        >
          <X className='size-3' aria-hidden='true' />
        </Button>
      )}
    </div>
  )
}

interface ErrorStatsTableProps {
  items: ErrorStatItem[]
  total: number
  isLoading: boolean
  savingHash: string | null
  onSaveShortName: (item: ErrorStatItem, shortName: string) => void
  onClearShortName: (item: ErrorStatItem) => void
  onDrillDown: (item: ErrorStatItem) => void
}

function formatPercentage(count: number, total: number): string {
  if (!total) return '0%'
  return `${((count / total) * 100).toFixed(1)}%`
}

export function ErrorStatsTable(props: ErrorStatsTableProps) {
  const { t } = useTranslation()

  return (
    <div className='h-full overflow-auto rounded-lg border'>
      <Table>
        <TableHeader className='bg-muted/50 sticky top-0 z-10'>
          <TableRow>
            <TableHead className='min-w-[160px]'>
              {t('Detailed Error Type')}
            </TableHead>
            <TableHead className='min-w-[280px]'>
              {t('Detailed Error')}
            </TableHead>
            <TableHead className='min-w-[120px]'>{t('Occurrences')}</TableHead>
            <TableHead className='min-w-[180px]'>{t('First Seen')}</TableHead>
            <TableHead className='min-w-[100px]'>{t('Status Code')}</TableHead>
            <TableHead className='min-w-[180px]'>{t('Models')}</TableHead>
            <TableHead className='min-w-[180px]'>{t('Channels')}</TableHead>
            <TableHead className='min-w-[120px] text-right'>
              {t('Actions')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.isLoading && (
            <TableRow>
              <TableCell colSpan={8} className='h-24 text-center'>
                {t('Loading...')}
              </TableCell>
            </TableRow>
          )}

          {!props.isLoading && props.items.length === 0 && (
            <TableRow>
              <TableCell
                colSpan={8}
                className='text-muted-foreground h-24 text-center'
              >
                {t('No error stats')}
              </TableCell>
            </TableRow>
          )}

          {!props.isLoading &&
            props.items.map((item) => (
              <TableRow key={item.signature_hash}>
                <TableCell>
                  <ShortNameCell
                    item={item}
                    isSaving={props.savingHash === item.signature_hash}
                    onSave={(shortName) =>
                      props.onSaveShortName(item, shortName)
                    }
                    onClear={() => props.onClearShortName(item)}
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
                  <div className='flex flex-col'>
                    <span className='font-medium tabular-nums'>
                      {item.count}
                    </span>
                    <span className='text-muted-foreground text-xs tabular-nums'>
                      {formatPercentage(item.count, props.total)}
                    </span>
                  </div>
                </TableCell>
                <TableCell>
                  <div className='flex flex-col text-xs tabular-nums'>
                    <span>{formatSeconds(item.first_seen)}</span>
                    <span className='text-muted-foreground'>
                      {formatSeconds(item.last_seen)}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='tabular-nums'>
                  {item.status_code || '-'}
                </TableCell>
                <TableCell>
                  <div className='flex flex-wrap gap-1'>
                    {item.models.length === 0 && (
                      <span className='text-muted-foreground text-xs'>-</span>
                    )}
                    {item.models.map((m) => (
                      <Badge
                        key={m.model}
                        variant='outline'
                        className='max-w-[180px]'
                      >
                        <span className='truncate'>{m.model}</span>
                        <span className='text-muted-foreground'>
                          ×{m.count}
                        </span>
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell>
                  <div className='flex flex-wrap gap-1'>
                    {item.channels.length === 0 && (
                      <span className='text-muted-foreground text-xs'>-</span>
                    )}
                    {item.channels.map((c) => (
                      <Badge
                        key={c.channel_id}
                        variant='outline'
                        className='max-w-[180px]'
                      >
                        <span className='truncate'>
                          {c.channel_name || `#${c.channel_id}`}
                        </span>
                        <span className='text-muted-foreground'>
                          ×{c.count}
                        </span>
                      </Badge>
                    ))}
                  </div>
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
        </TableBody>
      </Table>
    </div>
  )
}
