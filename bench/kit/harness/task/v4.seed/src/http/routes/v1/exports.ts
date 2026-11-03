import { json } from '../../respond'
import { capabilityWithheld, formatUnknown, malformed, reportUnknown } from '../../../errors/httpError'
import { countRows } from '../../../usage/meter'
import { exportCapability } from '../../../plan/capability'
import { getExport, getReport, putExport } from '../../../db/memory'
import { holds } from '../../../plan/resolve'
import { isExportFormat } from '../../../export/formats'
import { isoOf } from '../../../util/clock'
import { nextId } from '../../../util/ids'
import { parseObject, readString } from '../../../util/json'
import { render } from '../../../export/writer'
import type { Context } from '../../context'

export async function createExport(context: Context): Promise<Response> {
  const body = parseObject(await context.request.text())
  if (body === undefined) throw malformed('the body must be a JSON object')

  const reportId = readString(body, 'reportId')
  const format = readString(body, 'format')
  if (reportId === undefined || format === undefined) throw malformed('reportId and format are required')
  if (!isExportFormat(format)) throw formatUnknown(format)

  const capability = exportCapability(format)
  if (!holds(context.account.plan, capability)) throw capabilityWithheld(capability, context.account.plan)

  const report = getReport(reportId)
  if (report === undefined || report.accountId !== context.account.id) throw reportUnknown(reportId)

  const rendered = render(format, report.rows)
  countRows(context.account.id, report.rows.length)
  const record = putExport({
    id: nextId('exp'),
    accountId: context.account.id,
    reportId,
    format,
    state: 'ready',
    bytes: rendered.length,
    createdAt: isoOf(Date.now()),
  })
  return json({ id: record.id, state: record.state, bytes: record.bytes }, 201, context.requestId)
}

export function readExport(context: Context): Response {
  const record = getExport(context.params.id as string)
  if (record === undefined || record.accountId !== context.account.id) {
    throw reportUnknown(context.params.id as string)
  }
  return json(record, 200, context.requestId)
}
