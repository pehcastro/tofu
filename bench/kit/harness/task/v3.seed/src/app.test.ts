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
