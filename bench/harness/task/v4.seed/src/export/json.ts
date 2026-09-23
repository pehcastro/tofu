import type { Row } from '../db/memory'

export function toJSON(rows: Row[]): string {
  return JSON.stringify(rows)
}

export function toNDJSON(rows: Row[]): string {
  return rows.map((row) => JSON.stringify(row)).join('\n')
}
