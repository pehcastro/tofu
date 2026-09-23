export type Clock = () => number

export const systemClock: Clock = () => Date.now()

export function fixedClock(at: number): Clock {
  return () => at
}

export function isoOf(at: number): string {
  return new Date(at).toISOString()
}

export function startOfMinute(at: number): number {
  return at - (at % 60_000)
}

export function startOfDay(at: number): number {
  return at - (at % 86_400_000)
}
