import { describe, expect, it, beforeEach } from 'bun:test'
import { createApp } from './app'
import { TaskStore, type Task } from './store'

let app: ReturnType<typeof createApp>

beforeEach(() => {
  app = createApp(new TaskStore())
})

function request(path: string, init?: RequestInit) {
  return app.request(`http://localhost${path}`, init)
}

function postJson(path: string, body: unknown) {
  return request(path, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
}

function patchJson(path: string, body: unknown) {
  return request(path, {
    method: 'PATCH',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
}

async function createTask(title = 'write tests'): Promise<Task> {
  const res = await postJson('/tasks', { title })
  expect(res.status).toBe(201)
  return (await res.json()) as Task
}

describe('GET /', () => {
  it('still serves the original greeting', async () => {
    const res = await request('/')
    expect(res.status).toBe(200)
    expect(await res.text()).toBe('Hello Hono!')
  })
})

describe('POST /tasks', () => {
  it('creates a task and returns 201 with the created resource', async () => {
    const res = await postJson('/tasks', { title: 'buy milk' })
    expect(res.status).toBe(201)

    const task = (await res.json()) as Task
    expect(task).toMatchObject({ title: 'buy milk', done: false })
    expect(typeof task.id).toBe('string')
    expect(task.id.length).toBeGreaterThan(0)
    expect(typeof task.createdAt).toBe('string')
    expect(typeof task.updatedAt).toBe('string')
  })

  it('trims surrounding whitespace from the title', async () => {
    const res = await postJson('/tasks', { title: '  padded  ' })
    expect(res.status).toBe(201)
    expect(((await res.json()) as Task).title).toBe('padded')
  })

  it('accepts an explicit done flag', async () => {
    const res = await postJson('/tasks', { title: 'already done', done: true })
    expect(res.status).toBe(201)
    expect(((await res.json()) as Task).done).toBe(true)
  })

  it('assigns a distinct id to every task', async () => {
    const a = await createTask('one')
    const b = await createTask('two')
    expect(a.id).not.toBe(b.id)
  })

  it('rejects a missing title with 400', async () => {
    const res = await postJson('/tasks', {})
    expect(res.status).toBe(400)

    const body = (await res.json()) as { error: { code: string; message: string } }
    expect(body.error.code).toBe('bad_request')
    expect(typeof body.error.message).toBe('string')
  })

  it('rejects an empty title with 400', async () => {
    const res = await postJson('/tasks', { title: '' })
    expect(res.status).toBe(400)
    expect((await res.json()) as { error: unknown }).toHaveProperty('error')
  })

  it('rejects a whitespace-only title with 400', async () => {
    const res = await postJson('/tasks', { title: '   ' })
    expect(res.status).toBe(400)
  })

  it('rejects a non-string title with 400', async () => {
    const res = await postJson('/tasks', { title: 42 })
    expect(res.status).toBe(400)
  })

  it('rejects an over-long title with 400', async () => {
    const res = await postJson('/tasks', { title: 'x'.repeat(201) })
    expect(res.status).toBe(400)
  })

  it('rejects a non-boolean done with 400', async () => {
    const res = await postJson('/tasks', { title: 'ok', done: 'yes' })
    expect(res.status).toBe(400)
  })

  it('rejects a non-object body with 400', async () => {
    const res = await postJson('/tasks', ['not', 'an', 'object'])
    expect(res.status).toBe(400)
  })

  it('rejects a malformed JSON body with 400', async () => {
    const res = await request('/tasks', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: '{ not json',
    })
    expect(res.status).toBe(400)
    expect((await res.json()) as { error: unknown }).toHaveProperty('error')
  })

  it('does not store a task that failed validation', async () => {
    await postJson('/tasks', { title: '' })
    const res = await request('/tasks')
    expect((await res.json()) as Task[]).toHaveLength(0)
  })
})

describe('GET /tasks', () => {
  it('returns an empty array when there are no tasks', async () => {
    const res = await request('/tasks')
    expect(res.status).toBe(200)
    expect((await res.json()) as Task[]).toEqual([])
  })

  it('returns all created tasks', async () => {
    await createTask('first')
    await createTask('second')

    const res = await request('/tasks')
    expect(res.status).toBe(200)

    const tasks = (await res.json()) as Task[]
    expect(tasks).toHaveLength(2)
    expect(tasks.map((t) => t.title)).toEqual(['first', 'second'])
  })

  it('is isolated per app instance', async () => {
    await createTask('only here')
    const other = createApp(new TaskStore())
    const res = await other.request('http://localhost/tasks')
    expect((await res.json()) as Task[]).toEqual([])
  })
})

describe('GET /tasks/:id', () => {
  it('returns the requested task', async () => {
    const created = await createTask('find me')

    const res = await request(`/tasks/${created.id}`)
    expect(res.status).toBe(200)
    expect((await res.json()) as Task).toEqual(created)
  })

  it('returns 404 with a JSON error for an unknown id', async () => {
    const res = await request('/tasks/does-not-exist')
    expect(res.status).toBe(404)

    const body = (await res.json()) as { error: { code: string; message: string } }
    expect(body.error.code).toBe('not_found')
    expect(body.error.message).toContain('does-not-exist')
  })
})

describe('PATCH /tasks/:id', () => {
  it('updates the title only', async () => {
    const created = await createTask('old title')

    const res = await patchJson(`/tasks/${created.id}`, { title: 'new title' })
    expect(res.status).toBe(200)

    const updated = (await res.json()) as Task
    expect(updated.title).toBe('new title')
    expect(updated.done).toBe(created.done)
    expect(updated.id).toBe(created.id)
  })

  it('updates the done flag only', async () => {
    const created = await createTask('toggle me')

    const res = await patchJson(`/tasks/${created.id}`, { done: true })
    expect(res.status).toBe(200)

    const updated = (await res.json()) as Task
    expect(updated.done).toBe(true)
    expect(updated.title).toBe(created.title)
  })

  it('updates both fields at once', async () => {
    const created = await createTask('both')

    const res = await patchJson(`/tasks/${created.id}`, { title: 'renamed', done: true })
    expect(res.status).toBe(200)
    expect((await res.json()) as Task).toMatchObject({ title: 'renamed', done: true })
  })

  it('persists the update for later reads', async () => {
    const created = await createTask('persist me')
    await patchJson(`/tasks/${created.id}`, { done: true })

    const res = await request(`/tasks/${created.id}`)
    expect(((await res.json()) as Task).done).toBe(true)
  })

  it('rejects an empty payload with 400', async () => {
    const created = await createTask()
    const res = await patchJson(`/tasks/${created.id}`, {})
    expect(res.status).toBe(400)
    expect((await res.json()) as { error: unknown }).toHaveProperty('error')
  })

  it('rejects an empty title with 400', async () => {
    const created = await createTask()
    const res = await patchJson(`/tasks/${created.id}`, { title: '   ' })
    expect(res.status).toBe(400)
  })

  it('rejects a non-boolean done with 400', async () => {
    const created = await createTask()
    const res = await patchJson(`/tasks/${created.id}`, { done: 'nope' })
    expect(res.status).toBe(400)
  })

  it('rejects a malformed JSON body with 400', async () => {
    const created = await createTask()
    const res = await request(`/tasks/${created.id}`, {
      method: 'PATCH',
      headers: { 'content-type': 'application/json' },
      body: '{ not json',
    })
    expect(res.status).toBe(400)
  })

  it('leaves the task untouched when validation fails', async () => {
    const created = await createTask('unchanged')
    await patchJson(`/tasks/${created.id}`, { title: '' })

    const res = await request(`/tasks/${created.id}`)
    expect((await res.json()) as Task).toEqual(created)
  })

  it('returns 404 with a JSON error for an unknown id', async () => {
    const res = await patchJson('/tasks/999', { title: 'ghost' })
    expect(res.status).toBe(404)
    expect((await res.json()) as { error: { code: string } }).toMatchObject({
      error: { code: 'not_found' },
    })
  })
})

describe('DELETE /tasks/:id', () => {
  it('removes the task and returns 204', async () => {
    const created = await createTask('delete me')

    const res = await request(`/tasks/${created.id}`, { method: 'DELETE' })
    expect(res.status).toBe(204)
    expect(await res.text()).toBe('')

    const after = await request(`/tasks/${created.id}`)
    expect(after.status).toBe(404)
  })

  it('leaves the other tasks in place', async () => {
    const keep = await createTask('keep')
    const drop = await createTask('drop')

    await request(`/tasks/${drop.id}`, { method: 'DELETE' })

    const res = await request('/tasks')
    const tasks = (await res.json()) as Task[]
    expect(tasks).toHaveLength(1)
    expect(tasks[0]!.id).toBe(keep.id)
  })

  it('returns 404 with a JSON error for an unknown id', async () => {
    const res = await request('/tasks/nope', { method: 'DELETE' })
    expect(res.status).toBe(404)
    expect((await res.json()) as { error: { code: string } }).toMatchObject({
      error: { code: 'not_found' },
    })
  })

  it('returns 404 when deleting the same task twice', async () => {
    const created = await createTask()
    await request(`/tasks/${created.id}`, { method: 'DELETE' })

    const res = await request(`/tasks/${created.id}`, { method: 'DELETE' })
    expect(res.status).toBe(404)
  })
})
