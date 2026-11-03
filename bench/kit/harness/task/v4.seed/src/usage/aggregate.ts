import type { Row } from '../db/memory'

export function groupRows(rows: Row[], groupBy: string[]): Row[] {
  if (groupBy.length === 0) return rows
  const buckets = new Map<string, Row>()
  for (const row of rows) {
    const key = groupBy.map((field) => String(row[field] ?? '')).join('\u0000')
    const bucket = buckets.get(key)
    if (bucket === undefined) {
      buckets.set(key, { ...row })
      continue
    }
    for (const [field, value] of Object.entries(row)) {
      if (typeof value === 'number' && typeof bucket[field] === 'number') {
        bucket[field] = (bucket[field] as number) + value
      }
    }
  }
  return [...buckets.values()]
}

export function columnsOf(rows: Row[]): string[] {
  const columns = new Set<string>()
  for (const row of rows) {
    for (const field of Object.keys(row)) columns.add(field)
  }
  return [...columns]
}
