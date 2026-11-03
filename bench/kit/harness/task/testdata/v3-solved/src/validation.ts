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

function readExpiresAt(raw: unknown, now: number): string | null | Rejected {
  if (raw === null) return null
  if (typeof raw !== 'string') return { rejected: 'expiresAt must be an ISO 8601 timestamp or null' }
  const at = Date.parse(raw)
  if (Number.isNaN(at)) return { rejected: 'expiresAt must be an ISO 8601 timestamp' }
  if (at <= now) return { rejected: 'expiresAt must be in the future' }
  return new Date(at).toISOString()
}

export function readExpiredFilter(raw: string | null): boolean | Rejected {
  if (raw === null || raw === 'false') return false
  if (raw === 'true') return true
  return { rejected: 'expired must be true or false' }
}

export function readCreate(input: unknown, now: number = Date.now()): CreateNoteInput | Rejected {
  const object = asObject(input)
  if (isRejected(object)) return object
  const title = readTitle(object.title)
  if (isRejected(title)) return title
  const body = readBody(object.body ?? '')
  if (isRejected(body)) return body
  const created: CreateNoteInput = { title, body }
  if ('expiresAt' in object) {
    const expiresAt = readExpiresAt(object.expiresAt, now)
    if (isRejected(expiresAt)) return expiresAt
    created.expiresAt = expiresAt
  }
  return created
}

export function readUpdate(input: unknown, now: number = Date.now()): UpdateNoteInput | Rejected {
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
  if ('expiresAt' in object) {
    const expiresAt = readExpiresAt(object.expiresAt, now)
    if (isRejected(expiresAt)) return expiresAt
    update.expiresAt = expiresAt
  }
  if (Object.keys(update).length === 0) {
    return { rejected: 'the body must carry a title, a body or an expiresAt' }
  }
  return update
}
