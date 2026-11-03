import { describe, expect, test } from 'bun:test'
import { handle } from '../../server'

const token = 'tok_wren'

function sendJSON(path: string, body: unknown, bearer: string | null = token) {
  const headers: Record<string, string> = { 'content-type': 'application/json' }
  if (bearer !== null) headers.authorization = `Bearer ${bearer}`
  return handle(path, { method: 'POST', headers, body: JSON.stringify(body) })
}

async function reportId(): Promise<string> {
  const res = await sendJSON('/v1/reports', { range: { from: '2026-09-01', to: '2026-09-03' }, groupBy: ['route'] })
  expect(res.status).toBe(201)
  return (await res.json()).id
}

describe('POST /v1/exports', () => {
  test('renders a report the account is entitled to', async () => {
    const res = await sendJSON('/v1/exports', { reportId: await reportId(), format: 'csv' })
    expect(res.status).toBe(201)
    const created = await res.json()
    expect(created.state).toBe('ready')
    expect(created.bytes).toBeGreaterThan(0)
  })

  test('refuses a format this build does not know', async () => {
    const res = await sendJSON('/v1/exports', { reportId: await reportId(), format: 'toml' })
    expect(res.status).toBe(400)
    expect((await res.json()).error.code).toBe('export.format.unknown')
  })

  test('refuses a report that belongs to nobody', async () => {
    const res = await sendJSON('/v1/exports', { reportId: 'rep_zzzz', format: 'csv' })
    expect(res.status).toBe(404)
  })

  test('refuses a request with no credential', async () => {
    const res = await sendJSON('/v1/exports', { reportId: 'rep_zzzz', format: 'csv' }, null)
    expect(res.status).toBe(401)
  })
})

describe('GET /v1/health', () => {
  test('needs no credential', async () => {
    const res = await handle('/v1/health')
    expect(res.status).toBe(200)
    expect((await res.json()).ok).toBe(true)
  })
})
