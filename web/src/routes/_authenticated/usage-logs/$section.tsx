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
import { createFileRoute, redirect } from '@tanstack/react-router'
import z from 'zod'

import { UsageLogs } from '@/features/usage-logs'
import {
  isUsageLogsSectionId,
  USAGE_LOGS_DEFAULT_SECTION,
} from '@/features/usage-logs/section-registry'

const logTypeValues = ['0', '1', '2', '3', '4', '5', '6', '7'] as const
const logTypeSearchSchema = z
  .preprocess(
    (value) => {
      if (value == null || value === '') return undefined
      return Array.isArray(value) ? value : [value]
    },
    z.array(z.enum(logTypeValues)).optional()
  )
  .catch([])

const usageLogsSearchSchema = z.object({
  view: z.enum(['list', 'stats']).optional().catch('list'),
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  type: logTypeSearchSchema.optional(),
  filter: z.string().optional().catch(''),
  model: z.string().optional().catch(''),
  modelOp: z.enum(['include', 'exclude']).optional().catch('include'),
  token: z.string().optional().catch(''),
  tokenOp: z.enum(['include', 'exclude']).optional().catch('include'),
  channel: z.string().optional().catch(''),
  channelOp: z.enum(['include', 'exclude']).optional().catch('include'),
  group: z.string().optional().catch(''),
  groupOp: z.enum(['include', 'exclude']).optional().catch('include'),
  username: z.string().optional().catch(''),
  usernameOp: z.enum(['include', 'exclude']).optional().catch('include'),
  requestId: z.string().optional().catch(''),
  upstreamRequestId: z.string().optional().catch(''),
  // Extra server-side log filters (module J): session id, downstream trace
  // id, upstream HTTP status code, and an error-content keyword.
  sessionId: z.string().optional().catch(''),
  downstreamRequestId: z.string().optional().catch(''),
  downstreamTraceTag: z.string().optional().catch(''),
  customerName: z.string().optional().catch(''),
  retryTriggered: z.enum(['true', 'false']).optional().catch(undefined),
  statusCode: z.string().optional().catch(''),
  errorKeyword: z.string().optional().catch(''),
  // Drill-down params carried by the detailed-error-stats page (module K).
  // errorSignatureHash is the indexed backend filter; errorSignature is a
  // display-only label.
  errorSignatureHash: z.string().optional().catch(''),
  errorSignature: z.string().optional().catch(''),
  startTime: z.number().optional(),
  endTime: z.number().optional(),
})

export const Route = createFileRoute('/_authenticated/usage-logs/$section')({
  beforeLoad: ({ params, search }) => {
    if (!isUsageLogsSectionId(params.section)) {
      throw redirect({
        to: '/usage-logs/$section',
        params: { section: USAGE_LOGS_DEFAULT_SECTION },
      })
    }
    // type 仅 common 使用，非 common 时清掉 URL 里的 type
    const hasTypeSearch = Array.isArray(search?.type)
      ? search.type.length > 0
      : search?.type != null && search.type !== ''
    if (params.section !== 'common' && hasTypeSearch) {
      throw redirect({
        to: '/usage-logs/$section',
        params: { section: params.section },
        search: { ...search, type: undefined },
        replace: true,
      })
    }
  },
  validateSearch: usageLogsSearchSchema,
  component: UsageLogs,
})
