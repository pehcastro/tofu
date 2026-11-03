export const EXPORT_FORMATS = ['csv', 'json', 'ndjson', 'parquet', 'xlsx'] as const

export type ExportFormat = (typeof EXPORT_FORMATS)[number]

export function isExportFormat(value: unknown): value is ExportFormat {
  return typeof value === 'string' && (EXPORT_FORMATS as readonly string[]).includes(value)
}

export const CONTENT_TYPES: Record<ExportFormat, string> = {
  csv: 'text/csv',
  json: 'application/json',
  ndjson: 'application/x-ndjson',
  parquet: 'application/vnd.apache.parquet',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
}

export const FILE_SUFFIXES: Record<ExportFormat, string> = {
  csv: '.csv',
  json: '.json',
  ndjson: '.ndjson',
  parquet: '.parquet',
  xlsx: '.xlsx',
}
