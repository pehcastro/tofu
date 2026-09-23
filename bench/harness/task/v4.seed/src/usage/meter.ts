import { startOfMinute, systemClock, type Clock } from '../util/clock'

export type Counted = {
  accountId: string
  minute: number
  requests: number
  rows: number
}

const counted = new Map<string, Counted>()

function keyOf(accountId: string, minute: number): string {
  return `${accountId}@${minute}`
}

export function countRequest(accountId: string, clock: Clock = systemClock): Counted {
  const minute = startOfMinute(clock())
  const key = keyOf(accountId, minute)
  const seen = counted.get(key) ?? { accountId, minute, requests: 0, rows: 0 }
  seen.requests += 1
  counted.set(key, seen)
  return seen
}

export function countRows(accountId: string, rows: number, clock: Clock = systemClock): void {
  const minute = startOfMinute(clock())
  const key = keyOf(accountId, minute)
  const seen = counted.get(key) ?? { accountId, minute, requests: 0, rows: 0 }
  seen.rows += rows
  counted.set(key, seen)
}

export function requestsThisMinute(accountId: string, clock: Clock = systemClock): number {
  return counted.get(keyOf(accountId, startOfMinute(clock())))?.requests ?? 0
}

export function rowsFor(accountId: string): number {
  let total = 0
  for (const seen of counted.values()) {
    if (seen.accountId === accountId) total += seen.rows
  }
  return total
}

export function resetMeter(): void {
  counted.clear()
}
