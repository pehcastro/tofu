const counters = new Map<string, number>()

export function nextId(prefix: string): string {
  const seen = (counters.get(prefix) ?? 0) + 1
  counters.set(prefix, seen)
  return `${prefix}_${seen.toString(36).padStart(4, '0')}`
}

export function resetIds(): void {
  counters.clear()
}
