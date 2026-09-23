import type { Account } from '../accounts/accounts'

export type Context = {
  request: Request
  url: URL
  params: Record<string, string>
  requestId: string
  account: Account
}

export async function bodyOf(context: Context): Promise<string> {
  return context.request.text()
}
