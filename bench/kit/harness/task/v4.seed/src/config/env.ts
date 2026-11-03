import { DEFAULTS, type Settings } from './defaults'

const numericKeys = ['port', 'requestsPerMinute', 'exportRowCeiling', 'reportWindowDays'] as const

const variableOf: Record<keyof Settings, string> = {
  port: 'METERAGE_PORT',
  requestsPerMinute: 'METERAGE_RPM',
  exportRowCeiling: 'METERAGE_EXPORT_ROW_CEILING',
  reportWindowDays: 'METERAGE_REPORT_WINDOW_DAYS',
  csvDelimiter: 'METERAGE_CSV_DELIMITER',
  csvLineEnding: 'METERAGE_CSV_LINE_ENDING',
  logLevel: 'METERAGE_LOG_LEVEL',
}

export function settingsFrom(environment: Record<string, string | undefined>): Settings {
  const settings: Settings = { ...DEFAULTS }
  for (const key of numericKeys) {
    const raw = environment[variableOf[key]]
    if (raw === undefined) continue
    const value = Number(raw)
    if (!Number.isFinite(value) || value <= 0) {
      throw new Error(`${variableOf[key]} is ${raw}, which is not a positive number`)
    }
    settings[key] = value
  }
  const delimiter = environment[variableOf.csvDelimiter]
  if (delimiter !== undefined) settings.csvDelimiter = delimiter
  const level = environment[variableOf.logLevel]
  if (level === 'debug' || level === 'info' || level === 'warn' || level === 'error') settings.logLevel = level
  return settings
}

export const SETTINGS: Settings = settingsFrom(process.env)
