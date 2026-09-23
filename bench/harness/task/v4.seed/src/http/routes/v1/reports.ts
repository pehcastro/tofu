import { json } from '../../respond'
import { capabilityWithheld, malformed } from '../../../errors/httpError'
import { groupRows } from '../../../usage/aggregate'
import { holds } from '../../../plan/resolve'
import { isoOf } from '../../../util/clock'
import { nextId } from '../../../util/ids'
import { parseObject, readString } from '../../../util/json'
import { putReport, type Row } from '../../../db/memory'
import type { Context } from '../../context'

const sampleRows: Row[] = [
  { day: '2026-09-01', route: '/v1/usage', requests: 1420, bytes: 88_100 },
  { day: '2026-09-01', route: '/v1/exports', requests: 31, bytes: 9_440_000 },
  { day: '2026-09-02', route: '/v1/usage', requests: 1655, bytes: 91_200 },
  { day: '2026-09-02', route: '/v1/exports', requests: 44, bytes: 12_880_000 },
]

export async function createReport(context: Context): Promise<Response> {
  if (!holds(context.account.plan, 'report:run')) {
    throw capabilityWithheld('report:run', context.account.plan)
  }
  const body = parseObject(await context.request.text())
  if (body === undefined) throw malformed('the body must be a JSON object')
  const range = body.range
  if (typeof range !== 'object' || range === null) throw malformed('range is required')
  const from = readString(range as Record<string, unknown>, 'from')
  const to = readString(range as Record<string, unknown>, 'to')
  if (from === undefined || to === undefined) throw malformed('range.from and range.to are required')
  const groupBy = Array.isArray(body.groupBy) ? body.groupBy.map(String) : []

  const report = putReport({
    id: nextId('rep'),
    accountId: context.account.id,
    from,
    to,
    groupBy,
    rows: groupRows(sampleRows, groupBy),
    createdAt: isoOf(Date.now()),
  })
  return json({ id: report.id, rows: report.rows.length }, 201, context.requestId)
}
