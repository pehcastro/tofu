import type { Context } from './context'

export type Handler = (context: Context) => Response | Promise<Response>

type Route = {
  method: string
  segments: string[]
  handler: Handler
  anonymous: boolean
}

export class Router {
  private routes: Route[] = []

  add(method: string, path: string, handler: Handler, anonymous = false): void {
    this.routes.push({ method, segments: path.split('/').filter((s) => s !== ''), handler, anonymous })
  }

  match(method: string, pathname: string): { route: Route; params: Record<string, string> } | undefined {
    const parts = pathname.split('/').filter((s) => s !== '')
    for (const route of this.routes) {
      if (route.method !== method || route.segments.length !== parts.length) continue
      const params: Record<string, string> = {}
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
      if (matched) return { route, params }
    }
    return undefined
  }
}
