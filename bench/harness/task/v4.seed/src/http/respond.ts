import { HttpError } from '../errors/httpError'
import { ErrorCodes } from '../errors/codes'

export function json(body: unknown, status = 200, requestId = ''): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', 'x-request-id': requestId },
  })
}

export function problem(error: unknown, requestId = ''): Response {
  if (error instanceof HttpError) {
    return json({ error: { code: error.code, message: error.message, ...error.detail } }, error.status, requestId)
  }
  return json({ error: { code: ErrorCodes.Fault, message: 'something went wrong' } }, 500, requestId)
}
