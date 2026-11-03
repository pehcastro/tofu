import { describe, expect, test } from 'bun:test'
import { capabilitiesOf, exportFormatsOf, holds, withheldFrom } from './resolve'

describe('what a plan carries', () => {
  test('free reads usage and runs a report', () => {
    expect(holds('free', 'usage:read')).toBe(true)
    expect(holds('free', 'report:run')).toBe(true)
  })

  test('free cannot schedule a report even though the table grants it', () => {
    expect(withheldFrom('free')).toContain('report:schedule')
    expect(holds('free', 'report:schedule')).toBe(false)
  })

  test('silver has parquet withheld', () => {
    expect(exportFormatsOf('silver')).toEqual(['csv', 'json'])
  })

  test('enterprise has nothing withheld', () => {
    expect(withheldFrom('enterprise')).toEqual([])
    expect(capabilitiesOf('enterprise')).toContain('account:write')
  })
})
