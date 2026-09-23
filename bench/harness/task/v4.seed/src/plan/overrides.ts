import type { Capability } from './capability'
import type { PlanName } from './plans'

export const withheldByPlan: Record<PlanName, readonly Capability[]> = {
  free: ['report:schedule'],
  silver: ['export:parquet'],
  gold: ['export:csv', 'export:xlsx'],
  enterprise: [],
}

export const withheldSince: Record<PlanName, string> = {
  free: '2025-11-04',
  silver: '2026-01-19',
  gold: '2026-02-27',
  enterprise: '',
}

export function isWithheld(plan: PlanName, capability: Capability): boolean {
  return withheldByPlan[plan].includes(capability)
}
