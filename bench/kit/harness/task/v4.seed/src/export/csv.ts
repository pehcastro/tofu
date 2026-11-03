import { columnsOf } from '../usage/aggregate'
import { SETTINGS } from '../config/env'
import type { Row } from '../db/memory'

function quote(value: string, delimiter: string): string {
  if (!value.includes(delimiter) && !value.includes('"') && !value.includes('\n')) return value
  return `"${value.replaceAll('"', '""')}"`
}

export function toCSV(rows: Row[]): string {
  const delimiter = SETTINGS.csvDelimiter
  const columns = columnsOf(rows)
  const lines = [columns.map((column) => quote(column, delimiter)).join(delimiter)]
  for (const row of rows) {
    lines.push(columns.map((column) => quote(String(row[column] ?? ''), delimiter)).join(delimiter))
  }
  return lines.join(SETTINGS.csvLineEnding)
}
