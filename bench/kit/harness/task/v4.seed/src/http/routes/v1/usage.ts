import { json } from '../../respond'
import { SETTINGS } from '../../../config/env'
import { capabilityWithheld } from '../../../errors/httpError'
import { holds } from '../../../plan/resolve'
import { requestsThisMinute, rowsFor } from '../../../usage/meter'
import { windowEnding } from '../../../usage/window'
import type { Context } from '../../context'

export function usage(context: Context): Response {
  if (!holds(context.account.plan, 'usage:read')) {
    throw capabilityWithheld('usage:read', context.account.plan)
  }
  return json(
    {
      window: windowEnding(Date.now(), SETTINGS.reportWindowDays),
      rows: rowsFor(context.account.id),
      requestsThisMinute: requestsThisMinute(context.account.id),
    },
    200,
    context.requestId,
  )
}
