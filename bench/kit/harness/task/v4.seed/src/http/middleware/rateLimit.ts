import { SETTINGS } from '../../config/env'
import { countRequest } from '../../usage/meter'
import { rateExceeded } from '../../errors/httpError'
import type { Account } from '../../accounts/accounts'

export function enforceRate(account: Account): void {
  const counted = countRequest(account.id)
  if (counted.requests > SETTINGS.requestsPerMinute) throw rateExceeded(SETTINGS.requestsPerMinute)
}
