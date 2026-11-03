import { Router, json } from './router'
import { NoteStore } from './store'
import { isRejected, readCreate, readUpdate } from './validation'

async function readJSON(request: Request): Promise<unknown | undefined> {
  try {
    return await request.json()
  } catch {
    return undefined
  }
}

const notFound = () => json({ error: 'no such note' }, 404)

export function createApp(): Router {
  const app = new Router()
  const store = new NoteStore()

  app.add('GET', '/', () => new Response('notes'))

  app.add('POST', '/notes', async (request) => {
    const body = await readJSON(request)
    if (body === undefined) return json({ error: 'the body must be JSON' }, 400)
    const input = readCreate(body)
    if (isRejected(input)) return json({ error: input.rejected }, 400)
    return json(store.create(input), 201)
  })

  app.add('GET', '/notes', () => json(store.list()))

  app.add('GET', '/notes/:id', (_request, params) => {
    const note = store.get(params.id as string)
    return note === undefined ? notFound() : json(note)
  })

  app.add('PATCH', '/notes/:id', async (request, params) => {
    const body = await readJSON(request)
    if (body === undefined) return json({ error: 'the body must be JSON' }, 400)
    const input = readUpdate(body)
    if (isRejected(input)) return json({ error: input.rejected }, 400)
    const note = store.update(params.id as string, input)
    return note === undefined ? notFound() : json(note)
  })

  app.add('DELETE', '/notes/:id', (_request, params) => {
    return store.remove(params.id as string) ? json({ removed: params.id }) : notFound()
  })

  return app
}
