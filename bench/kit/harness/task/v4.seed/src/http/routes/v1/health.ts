import { json } from '../../respond'
import type { Context } from '../../context'

export function health(context: Context): Response {
  return json({ ok: true }, 200, context.requestId)
}
