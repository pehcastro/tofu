import { Hono } from 'hono'
import { TaskStore, type Task } from './store'

type Variables = {
  store: TaskStore
}

const MAX_TITLE_LENGTH = 200

function badRequest(message: string, details?: string[]) {
  return { error: { code: 'bad_request', message, ...(details ? { details } : {}) } }
}

function notFound(id: string) {
  return { error: { code: 'not_found', message: `Task '${id}' not found` } }
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** Reads the request body as JSON, or `undefined` when it isn't valid JSON. */
async function readJson(c: { req: { json(): Promise<unknown> } }): Promise<unknown | undefined> {
  try {
    return await c.req.json()
  } catch {
    return undefined
  }
}

/** Validates a `title` value, returning the normalised title or an error message. */
function parseTitle(value: unknown): { ok: true; value: string } | { ok: false; error: string } {
  if (typeof value !== 'string') return { ok: false, error: '"title" must be a string' }
  const title = value.trim()
  if (title.length === 0) return { ok: false, error: '"title" must not be empty' }
  if (title.length > MAX_TITLE_LENGTH) {
    return { ok: false, error: `"title" must be at most ${MAX_TITLE_LENGTH} characters` }
  }
  return { ok: true, value: title }
}

export function createApp(store: TaskStore = new TaskStore()) {
  const app = new Hono<{ Variables: Variables }>()

  app.get('/', (c) => {
    return c.text('Hello Hono!')
  })

  app.post('/tasks', async (c) => {
    const body = await readJson(c)
    if (!isPlainObject(body)) {
      return c.json(badRequest('Request body must be a JSON object'), 400)
    }

    const title = parseTitle(body.title)
    if (!title.ok) {
      return c.json(badRequest('Invalid task payload', [title.error]), 400)
    }

    if (body.done !== undefined && typeof body.done !== 'boolean') {
      return c.json(badRequest('Invalid task payload', ['"done" must be a boolean']), 400)
    }

    const task: Task = store.create({ title: title.value, done: body.done as boolean | undefined })
    return c.json(task, 201)
  })

  app.get('/tasks', (c) => {
    return c.json(store.list())
  })

  app.get('/tasks/:id', (c) => {
    const id = c.req.param('id')
    const task = store.get(id)
    if (!task) return c.json(notFound(id), 404)
    return c.json(task)
  })

  app.patch('/tasks/:id', async (c) => {
    const id = c.req.param('id')

    const body = await readJson(c)
    if (!isPlainObject(body)) {
      return c.json(badRequest('Request body must be a JSON object'), 400)
    }

    const details: string[] = []
    let title: string | undefined

    if (body.title !== undefined) {
      const parsed = parseTitle(body.title)
      if (parsed.ok) title = parsed.value
      else details.push(parsed.error)
    }

    if (body.done !== undefined && typeof body.done !== 'boolean') {
      details.push('"done" must be a boolean')
    }

    if (body.title === undefined && body.done === undefined) {
      details.push('At least one of "title" or "done" must be provided')
    }

    if (details.length > 0) {
      return c.json(badRequest('Invalid task payload', details), 400)
    }

    // Validation passed, so a missing task is the only remaining failure mode.
    const updated = store.update(id, { title, done: body.done as boolean | undefined })
    if (!updated) return c.json(notFound(id), 404)
    return c.json(updated)
  })

  app.delete('/tasks/:id', (c) => {
    const id = c.req.param('id')
    const deleted = store.delete(id)
    if (!deleted) return c.json(notFound(id), 404)
    return c.body(null, 204)
  })

  return app
}
