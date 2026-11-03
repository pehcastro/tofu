export const ErrorCodes = {
  Malformed: 'request.malformed',
  CredentialRejected: 'auth.credential.rejected',
  AccountUnknown: 'account.unknown',
  AccountSuspended: 'account.suspended',
  CapabilityWithheld: 'plan.capability.withheld',
  SeatCeiling: 'plan.seats.exceeded',
  RowCeiling: 'plan.rows.exceeded',
  RateExceeded: 'usage.rate.exceeded',
  FormatUnknown: 'export.format.unknown',
  ReportUnknown: 'report.unknown',
  RouteUnknown: 'route.unknown',
  Fault: 'server.fault',
} as const

export type ErrorCode = (typeof ErrorCodes)[keyof typeof ErrorCodes]
