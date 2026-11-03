import { toColumns, toParquet, toXLSX } from './columnar'
import { toCSV } from './csv'
import { toJSON, toNDJSON } from './json'
import type { ExportFormat } from './formats'
import type { Row } from '../db/memory'

export function render(format: ExportFormat, rows: Row[]): string {
  switch (format) {
    case 'csv':
      return toCSV(rows)
    case 'json':
      return toJSON(rows)
    case 'ndjson':
      return toNDJSON(rows)
    case 'parquet':
      return toParquet(rows)
    case 'xlsx':
      return toXLSX(rows)
  }
}

export function blockCount(rows: Row[]): number {
  return toColumns(rows).length
}
