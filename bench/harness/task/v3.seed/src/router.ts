export type Params = Record<string, string>

export type Handler = (request: Request, params: Params, url: URL) => Response | Promise<Response>

type Route = { method: string; segments: string[]; handler: Handler }

const base = 'http://notes.test'

export function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })
}

export class Router {
  private routes: Route[] = []

  add(method: string, path: string, handler: Handler): void {
    this.routes.push({ method, segments: path.split('/').filter((s) => s !== ''), handler })
  }

  async request(path: string, init?: RequestInit): Promise<Response> {
    const url = new URL(path, base)
    const request = new Request(url, init)
    const parts = url.pathname.split('/').filter((s) => s !== '')
    for (const route of this.routes) {
      if (route.method !== request.method || route.segments.length !== parts.length) continue
      const params: Params = {}
      let matched = true
      for (let i = 0; i < route.segments.length; i++) {
        const segment = route.segments[i] as string
        const part = parts[i] as string
        if (segment.startsWith(':')) {
          params[segment.slice(1)] = part
          continue
        }
        if (segment !== part) {
          matched = false
          break
        }
      }
      if (matched) return route.handler(request, params, url)
    }
    return json({ error: 'no such route' }, 404)
  }
}
