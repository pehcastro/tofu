import { ErrorCodes, type ErrorCode } from './codes'

export class HttpError extends Error {
  constructor(
    readonly status: number,
    readonly code: ErrorCode,
    message: string,
    readonly detail: Record<string, unknown> = {},
  ) {
    super(message)
    this.name = 'HttpError'
  }
}

export function malformed(message: string): HttpError {
  return new HttpError(400, ErrorCodes.Malformed, message)
}

export function credentialRejected(): HttpError {
  return new HttpError(401, ErrorCodes.CredentialRejected, 'the bearer token names no account')
}

export function accountSuspended(accountId: string): HttpError {
  return new HttpError(403, ErrorCodes.AccountSuspended, 'this account is suspended', { accountId })
}

export function capabilityWithheld(capability: string, plan: string): HttpError {
  return new HttpError(403, ErrorCodes.CapabilityWithheld, `the ${plan} plan does not carry ${capability}`, {
    capability,
    plan,
  })
}

export function formatUnknown(format: string): HttpError {
  return new HttpError(400, ErrorCodes.FormatUnknown, `this build does not know the export format ${format}`, { format })
}

export function rateExceeded(perMinute: number): HttpError {
  return new HttpError(429, ErrorCodes.RateExceeded, `over ${perMinute} requests a minute`, { perMinute })
}

export function reportUnknown(reportId: string): HttpError {
  return new HttpError(404, ErrorCodes.ReportUnknown, 'no such report', { reportId })
}
