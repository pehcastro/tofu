import { ACCOUNTS, type Account } from './accounts'

const byId = new Map<string, Account>(ACCOUNTS.map((account) => [account.id, account]))

const byToken = new Map<string, Account>(ACCOUNTS.map((account) => [account.token, account]))

export function accountById(id: string): Account | undefined {
  return byId.get(id)
}

export function accountByToken(token: string): Account | undefined {
  return byToken.get(token)
}

export function activeAccounts(): Account[] {
  return ACCOUNTS.filter((account) => account.active)
}
