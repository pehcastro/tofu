import { EXPORT_FORMATS, type ExportFormat } from '../export/formats'

export type ExportCapability = `export:${ExportFormat}`

export type Capability = ExportCapability | 'usage:read' | 'report:run' | 'report:schedule' | 'account:write'

export function exportCapability(format: ExportFormat): ExportCapability {
  return `export:${format}`
}

export const EXPORT_CAPABILITIES: readonly ExportCapability[] = EXPORT_FORMATS.map(exportCapability)

export const EVERY_CAPABILITY: readonly Capability[] = [
  'usage:read',
  'report:run',
  'report:schedule',
  'account:write',
  ...EXPORT_CAPABILITIES,
]

export function formatOf(capability: Capability): ExportFormat | undefined {
  if (!capability.startsWith('export:')) return undefined
  return capability.slice('export:'.length) as ExportFormat
}
