import { accountByToken } from '../../accounts/lookup'
import { accountSuspended, credentialRejected } from '../../errors/httpError'
import type { Account } from '../../accounts/accounts'

export function bearerOf(request: Request): string | undefined {
  const header = request.headers.get('authorization')
  if (header === null || !header.startsWith('Bearer ')) return undefined
  return header.slice('Bearer '.length).trim()
}

export function authenticate(request: Request): Account {
  const token = bearerOf(request)
  if (token === undefined) throw credentialRejected()
  const account = accountByToken(token)
  if (account === undefined) throw credentialRejected()
  if (!account.active) throw accountSuspended(account.id)
  return account
}
