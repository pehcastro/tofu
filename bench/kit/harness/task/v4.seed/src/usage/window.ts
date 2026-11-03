import { isoOf, startOfDay } from '../util/clock'

export type Window = {
  from: string
  to: string
  days: number
}

export function windowEnding(at: number, days: number): Window {
  const end = startOfDay(at) + 86_400_000
  return { from: isoOf(end - days * 86_400_000), to: isoOf(end), days }
}

export function isWithin(window: Window, at: string): boolean {
  const moment = Date.parse(at)
  return moment >= Date.parse(window.from) && moment < Date.parse(window.to)
}
