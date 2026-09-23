export type Row = Record<string, string | number>

export type Report = {
  id: string
  accountId: string
  from: string
  to: string
  groupBy: string[]
  rows: Row[]
  createdAt: string
}

export type ExportRecord = {
  id: string
  accountId: string
  reportId: string
  format: string
  state: 'running' | 'ready' | 'failed'
  bytes: number
  createdAt: string
}

const reports = new Map<string, Report>()

const exports_ = new Map<string, ExportRecord>()

export function putReport(report: Report): Report {
  reports.set(report.id, report)
  return report
}

export function getReport(id: string): Report | undefined {
  return reports.get(id)
}

export function putExport(record: ExportRecord): ExportRecord {
  exports_.set(record.id, record)
  return record
}

export function getExport(id: string): ExportRecord | undefined {
  return exports_.get(id)
}

export function clearAll(): void {
  reports.clear()
  exports_.clear()
}
