import { ErrorCodes } from '../errors/codes'
import { authenticate } from './middleware/auth'
import { enforceRate } from './middleware/rateLimit'
import { enforceSeats } from './middleware/seats'
import { json, problem } from './respond'
import { requestIdOf } from './middleware/requestId'
import { routes } from './routes'
import type { Account } from '../accounts/accounts'

const anonymous: Account = {
  id: 'acct_anonymous',
  name: 'anonymous',
  plan: 'free',
  seatsInUse: 0,
  active: true,
  token: '',
}

const router = routes()

const base = 'http://meterage.test'

export async function handle(path: string, init?: RequestInit): Promise<Response> {
  const url = new URL(path, base)
  const request = new Request(url, init)
  const requestId = requestIdOf(request)
  const matched = router.match(request.method, url.pathname)
  if (matched === undefined) {
    return json({ error: { code: ErrorCodes.RouteUnknown, message: 'no such route' } }, 404, requestId)
  }
  try {
    let account = anonymous
    if (!matched.route.anonymous) {
      account = authenticate(request)
      enforceRate(account)
      enforceSeats(account)
    }
    return await matched.route.handler({ request, url, params: matched.params, requestId, account })
  } catch (error) {
    return problem(error, requestId)
  }
}

export const app = { request: handle }
