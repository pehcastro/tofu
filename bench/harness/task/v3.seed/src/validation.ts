import type { CreateNoteInput, UpdateNoteInput } from './store'

export const MAX_TITLE_LENGTH = 120

export type Rejected = { rejected: string }

export function isRejected<T>(value: T | Rejected): value is Rejected {
  return typeof value === 'object' && value !== null && 'rejected' in value
}

function asObject(body: unknown): Record<string, unknown> | Rejected {
  if (typeof body !== 'object' || body === null || Array.isArray(body)) {
    return { rejected: 'the body must be a JSON object' }
  }
  return body as Record<string, unknown>
}

function readTitle(raw: unknown): string | Rejected {
  if (typeof raw !== 'string' || raw.trim() === '') return { rejected: 'title must be a non-empty string' }
  if (raw.length > MAX_TITLE_LENGTH) return { rejected: `title must be at most ${MAX_TITLE_LENGTH} characters` }
  return raw
}

function readBody(raw: unknown): string | Rejected {
  if (typeof raw !== 'string') return { rejected: 'body must be a string' }
  return raw
}

export function readCreate(input: unknown): CreateNoteInput | Rejected {
  const object = asObject(input)
  if (isRejected(object)) return object
  const title = readTitle(object.title)
  if (isRejected(title)) return title
  const body = readBody(object.body ?? '')
  if (isRejected(body)) return body
  return { title, body }
}

export function readUpdate(input: unknown): UpdateNoteInput | Rejected {
  const object = asObject(input)
  if (isRejected(object)) return object
  const update: UpdateNoteInput = {}
  if ('title' in object) {
    const title = readTitle(object.title)
    if (isRejected(title)) return title
    update.title = title
  }
  if ('body' in object) {
    const body = readBody(object.body)
    if (isRejected(body)) return body
    update.body = body
  }
  if (update.title === undefined && update.body === undefined) {
    return { rejected: 'the body must carry a title or a body' }
  }
  return update
}
