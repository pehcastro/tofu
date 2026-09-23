import { json } from '../../respond'
import { PLANS } from '../../../plan/plans'
import type { Context } from '../../context'

export function me(context: Context): Response {
  const plan = PLANS[context.account.plan]
  return json(
    {
      id: context.account.id,
      name: context.account.name,
      plan: plan.name,
      seats: plan.seats,
      seatsInUse: context.account.seatsInUse,
      monthlyRows: plan.monthlyRows,
    },
    200,
    context.requestId,
  )
}
