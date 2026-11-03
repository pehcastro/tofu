import { describe, expect, test } from 'bun:test'
import { createApp } from './app'
import { MAX_TITLE_LENGTH } from './validation'

function post(app: ReturnType<typeof createApp>, body: unknown) {
  return app.request('/notes', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
}

function patch(app: ReturnType<typeof createApp>, id: string, body: unknown) {
  return app.request(`/notes/${id}`, {
    method: 'PATCH',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
}

function inFuture(ms: number) {
  return new Date(Date.now() + ms).toISOString()
}

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

describe('the root route', () => {
  test('answers with the service name', async () => {
    const res = await createApp().request('/')
    expect(res.status).toBe(200)
    expect(await res.text()).toBe('notes')
  })
})

describe('creating a note', () => {
  test('returns the stored note', async () => {
    const res = await post(createApp(), { title: 'first', body: 'a body' })
    expect(res.status).toBe(201)
    const note = await res.json()
    expect(note.title).toBe('first')
    expect(note.body).toBe('a body')
    expect(typeof note.id).toBe('string')
    expect(typeof note.createdAt).toBe('string')
  })

  test('refuses a missing title', async () => {
    const res = await post(createApp(), {})
    expect(res.status).toBe(400)
    expect((await res.json()).error).toBeString()
  })

  test('refuses a title over the limit', async () => {
    const res = await post(createApp(), { title: 'a'.repeat(MAX_TITLE_LENGTH + 1) })
    expect(res.status).toBe(400)
  })

  test('refuses a body that is not JSON', async () => {
    const res = await createApp().request('/notes', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: 'not json',
    })
    expect(res.status).toBe(400)
  })
})

describe('reading notes', () => {
  test('lists the newest note first', async () => {
    const app = createApp()
    await post(app, { title: 'older', body: '' })
    await post(app, { title: 'newer', body: '' })
    const listed = await (await app.request('/notes')).json()
    expect(listed.map((note: { title: string }) => note.title)).toEqual(['newer', 'older'])
  })

  test('answers 404 for an id nobody issued', async () => {
    const res = await createApp().request('/notes/note_nothing')
    expect(res.status).toBe(404)
    expect((await res.json()).error).toBeString()
  })
})

describe('changing a note', () => {
  test('updates the title', async () => {
    const app = createApp()
    const created = await (await post(app, { title: 'before', body: '' })).json()
    const res = await patch(app, created.id, { title: 'after' })
    expect(res.status).toBe(200)
    expect((await res.json()).title).toBe('after')
  })

  test('refuses a body with nothing in it', async () => {
    const app = createApp()
    const created = await (await post(app, { title: 'kept', body: '' })).json()
    expect((await patch(app, created.id, {})).status).toBe(400)
  })

  test('answers 404 for an id nobody issued', async () => {
    expect((await patch(createApp(), 'note_nothing', { title: 'x' })).status).toBe(404)
  })
})

describe('removing a note', () => {
  test('makes it unreadable afterwards', async () => {
    const app = createApp()
    const created = await (await post(app, { title: 'doomed', body: '' })).json()
    expect((await app.request(`/notes/${created.id}`, { method: 'DELETE' })).status).toBe(200)
    expect((await app.request(`/notes/${created.id}`)).status).toBe(404)
  })

  test('answers 404 for an id nobody issued', async () => {
    expect((await createApp().request('/notes/note_nothing', { method: 'DELETE' })).status).toBe(404)
  })
})

describe('an expiry on a note', () => {
  test('is stored and read back', async () => {
    const at = inFuture(60_000)
    const created = await (await post(createApp(), { title: 'expiring', body: '', expiresAt: at })).json()
    expect(created.expiresAt).toBe(at)
  })

  test('defaults to null', async () => {
    const created = await (await post(createApp(), { title: 'permanent', body: '' })).json()
    expect(created.expiresAt).toBeNull()
  })

  test('refuses a value that is not a timestamp', async () => {
    expect((await post(createApp(), { title: 'bad', body: '', expiresAt: 'tomorrow' })).status).toBe(400)
  })

  test('refuses a value that has already passed', async () => {
    const res = await post(createApp(), { title: 'stale', body: '', expiresAt: '2001-01-01T00:00:00.000Z' })
    expect(res.status).toBe(400)
  })

  test('is set and cleared by PATCH', async () => {
    const app = createApp()
    const created = await (await post(app, { title: 'movable', body: '' })).json()
    const at = inFuture(60_000)
    expect((await (await patch(app, created.id, { expiresAt: at })).json()).expiresAt).toBe(at)
    expect((await (await patch(app, created.id, { expiresAt: null })).json()).expiresAt).toBeNull()
  })

  test('is refused by PATCH when it has already passed', async () => {
    const app = createApp()
    const created = await (await post(app, { title: 'movable', body: '' })).json()
    const res = await patch(app, created.id, { title: 'movable', expiresAt: '2001-01-01T00:00:00.000Z' })
    expect(res.status).toBe(400)
  })
})

describe('a note that has expired', () => {
  test('goes out of the default listing and the read the moment it passes', async () => {
    const app = createApp()
    const created = await (await post(app, { title: 'brief', body: '', expiresAt: inFuture(120) })).json()
    expect((await app.request(`/notes/${created.id}`)).status).toBe(200)
    await sleep(250)
    expect((await app.request(`/notes/${created.id}`)).status).toBe(404)
    const listed = await (await app.request('/notes')).json()
    expect(listed.map((note: { id: string }) => note.id)).not.toContain(created.id)
  })

  test('is still reachable through the expired listing', async () => {
    const app = createApp()
    const first = await (await post(app, { title: 'first out', body: '', expiresAt: inFuture(120) })).json()
    const second = await (await post(app, { title: 'second out', body: '', expiresAt: inFuture(240) })).json()
    const live = await (await post(app, { title: 'staying', body: '' })).json()
    await sleep(400)
    const expired = await (await app.request('/notes?expired=true')).json()
    const ids = expired.map((note: { id: string }) => note.id)
    expect(ids).toEqual([second.id, first.id])
    expect(ids).not.toContain(live.id)
  })

  test('is excluded by expired=false and the filter is validated', async () => {
    const app = createApp()
    const created = await (await post(app, { title: 'brief', body: '', expiresAt: inFuture(120) })).json()
    await sleep(250)
    const live = await (await app.request('/notes?expired=false')).json()
    expect(live.map((note: { id: string }) => note.id)).not.toContain(created.id)
    expect((await app.request('/notes?expired=maybe')).status).toBe(400)
  })
})
