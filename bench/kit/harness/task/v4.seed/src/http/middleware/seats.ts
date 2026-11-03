import { HttpError } from '../../errors/httpError'
import { ErrorCodes } from '../../errors/codes'
import { PLANS } from '../../plan/plans'
import type { Account } from '../../accounts/accounts'

export function enforceSeats(account: Account): void {
  const seats = PLANS[account.plan].seats
  if (account.seatsInUse > seats) {
    throw new HttpError(403, ErrorCodes.SeatCeiling, `${account.seatsInUse} seats in use and the plan carries ${seats}`, {
      plan: account.plan,
      seats,
    })
  }
}
