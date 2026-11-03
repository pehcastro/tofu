import { Router } from '../router'
import { createExport, readExport } from './v1/exports'
import { createReport } from './v1/reports'
import { health } from './v1/health'
import { me } from './v1/accounts'
import { usage } from './v1/usage'

export function routes(): Router {
  const router = new Router()
  router.add('GET', '/v1/health', health, true)
  router.add('GET', '/v1/accounts/me', me)
  router.add('GET', '/v1/usage', usage)
  router.add('POST', '/v1/reports', createReport)
  router.add('POST', '/v1/exports', createExport)
  router.add('GET', '/v1/exports/:id', readExport)
  return router
}
