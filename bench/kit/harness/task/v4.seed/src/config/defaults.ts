export type Settings = {
  port: number
  requestsPerMinute: number
  exportRowCeiling: number
  reportWindowDays: number
  csvDelimiter: string
  csvLineEnding: string
  logLevel: 'debug' | 'info' | 'warn' | 'error'
}

export const DEFAULTS: Settings = {
  port: 8787,
  requestsPerMinute: 240,
  exportRowCeiling: 2_000_000,
  reportWindowDays: 31,
  csvDelimiter: ',',
  csvLineEnding: '\n',
  logLevel: 'info',
}
