import { columnsOf } from '../usage/aggregate'
import type { Row } from '../db/memory'

export type ColumnBlock = {
  column: string
  values: (string | number)[]
}

export function toColumns(rows: Row[]): ColumnBlock[] {
  return columnsOf(rows).map((column) => ({
    column,
    values: rows.map((row) => row[column] ?? ''),
  }))
}

export function toParquet(rows: Row[]): string {
  return JSON.stringify({ encoding: 'parquet-stub', blocks: toColumns(rows) })
}

export function toXLSX(rows: Row[]): string {
  return JSON.stringify({ encoding: 'xlsx-stub', sheets: [{ name: 'usage', blocks: toColumns(rows) }] })
}
