import { nextId } from '../../util/ids'

export function requestIdOf(request: Request): string {
  return request.headers.get('x-request-id') ?? nextId('req')
}
