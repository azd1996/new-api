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
import { FileClockIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

// Build-time bundled product changelog (repo-root CHANGELOG.md).
import changelogContent from '../../../../../CHANGELOG.md?raw'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/ui/markdown'

export function ChangelogDialog() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  const content = changelogContent.trim()

  return (
    <>
      <Button type='button' variant='outline' onClick={() => setOpen(true)}>
        <FileClockIcon className='me-2 h-4 w-4' />
        {t('View changelog')}
      </Button>

      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('Changelog')}
        contentClassName='max-h-[80vh] overflow-y-auto'
        contentHeight='auto'
        bodyClassName='space-y-4'
        footer={
          <Button type='button' variant='secondary' onClick={() => setOpen(false)}>
            {t('Close')}
          </Button>
        }
      >
        {content ? (
          <Markdown>{content}</Markdown>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('No changelog available.')}
          </p>
        )}
      </Dialog>
    </>
  )
}
