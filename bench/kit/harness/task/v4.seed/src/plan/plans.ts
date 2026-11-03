import type { Capability } from './capability'

export const PLAN_NAMES = ['free', 'silver', 'gold', 'enterprise'] as const

export type PlanName = (typeof PLAN_NAMES)[number]

export type Grants = 'everything' | readonly Capability[]

export type Plan = {
  name: PlanName
  grants: Grants
  seats: number
  monthlyRows: number
  supportHours: number
}

export const PLANS: Record<PlanName, Plan> = {
  free: {
    name: 'free',
    grants: ['usage:read', 'report:run', 'export:csv'],
    seats: 1,
    monthlyRows: 50_000,
    supportHours: 0,
  },
  silver: {
    name: 'silver',
    grants: ['usage:read', 'report:run', 'report:schedule', 'export:csv', 'export:json'],
    seats: 5,
    monthlyRows: 500_000,
    supportHours: 8,
  },
  gold: {
    name: 'gold',
    grants: 'everything',
    seats: 20,
    monthlyRows: 5_000_000,
    supportHours: 40,
  },
  enterprise: {
    name: 'enterprise',
    grants: 'everything',
    seats: 500,
    monthlyRows: 100_000_000,
    supportHours: 720,
  },
}

export function isPlanName(value: unknown): value is PlanName {
  return typeof value === 'string' && (PLAN_NAMES as readonly string[]).includes(value)
}
