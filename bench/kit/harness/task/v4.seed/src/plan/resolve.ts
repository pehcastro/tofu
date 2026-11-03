import { EVERY_CAPABILITY, formatOf, type Capability } from './capability'
import { isWithheld, withheldByPlan } from './overrides'
import { PLANS, type PlanName } from './plans'
import type { ExportFormat } from '../export/formats'

export function capabilitiesOf(plan: PlanName): Capability[] {
  const grants = PLANS[plan].grants
  const granted = grants === 'everything' ? [...EVERY_CAPABILITY] : [...grants]
  return granted.filter((capability) => !isWithheld(plan, capability))
}

export function holds(plan: PlanName, capability: Capability): boolean {
  return capabilitiesOf(plan).includes(capability)
}

export function exportFormatsOf(plan: PlanName): ExportFormat[] {
  const formats: ExportFormat[] = []
  for (const capability of capabilitiesOf(plan)) {
    const format = formatOf(capability)
    if (format !== undefined) formats.push(format)
  }
  return formats.sort()
}

export function withheldFrom(plan: PlanName): readonly Capability[] {
  return withheldByPlan[plan]
}
