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
import { useTranslation } from 'react-i18next'

import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { formatBytes } from '../lib/cache-skeleton'
import type { ByteCacheCoverage } from '../types'

function finite(value: number | undefined): value is number {
  return value !== undefined && Number.isFinite(value)
}

function signedBytes(value: number): string {
  if (value === 0) return '0B'
  return value > 0 ? `+${formatBytes(value)}` : `-${formatBytes(-value)}`
}

export function ByteCacheCoverageSummary(props: {
  coverage?: ByteCacheCoverage
  className?: string
}) {
  const { t, i18n } = useTranslation()
  const coverage = props.coverage
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const availableCoverage =
    coverage &&
    coverage.comparable_turns > 0 &&
    finite(coverage.baseline_covered_bytes) &&
    finite(coverage.optimized_covered_bytes) &&
    finite(coverage.delta_bytes) &&
    finite(coverage.total_bytes) &&
    coverage.total_bytes > 0

  const available = availableCoverage ? coverage : undefined

  return (
    <section
      aria-label={t('Estimated byte coverage')}
      className={props.className ?? 'w-full min-w-0 space-y-1 text-xs'}
    >
      <div className='flex flex-wrap items-center gap-x-4 gap-y-1'>
        <span className='font-semibold'>{t('Estimated byte coverage')}</span>
        {available ? (
          <>
            <span>
              {t('Baseline → optimized')}{' '}
              <b>
                {(
                  (available.baseline_covered_bytes / available.total_bytes) *
                  100
                ).toFixed(1)}
                %{' → '}
                {(
                  (available.optimized_covered_bytes / available.total_bytes) *
                  100
                ).toFixed(1)}
                %
              </b>
            </span>
            <span>
              {t('Change')}{' '}
              <b>
                {available.delta_bytes >= 0 ? '+' : ''}
                {(
                  (available.delta_bytes / available.total_bytes) *
                  100
                ).toFixed(1)}{' '}
                pp
                {' / '}
                {signedBytes(available.delta_bytes)}
              </b>
            </span>
            <span>
              {t('Comparable turns')}:{' '}
              <b>{formatNumber(available.comparable_turns, locale)}</b>
            </span>
          </>
        ) : (
          <span className='text-muted-foreground'>
            {t('No comparable samples recorded since upgrade')}
          </span>
        )}
      </div>
      <p className='text-muted-foreground'>
        {t(
          'Normalized estimated bytes exclude cache_control and compare independent histories. Actual token hit is shown separately.'
        )}
      </p>
    </section>
  )
}
