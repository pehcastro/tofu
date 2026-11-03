export type LegacyTier = 'starter' | 'pro' | 'gold' | 'unlimited'

export type LegacyEntitlement = {
  tier: LegacyTier
  exports: string[]
  seats: number
  rows: number
}

export const LEGACY_ENTITLEMENTS: Record<LegacyTier, LegacyEntitlement> = {
  starter: { tier: 'starter', exports: ['csv'], seats: 1, rows: 25_000 },
  pro: { tier: 'pro', exports: ['csv', 'json'], seats: 5, rows: 250_000 },
  gold: { tier: 'gold', exports: ['csv', 'json', 'ndjson', 'parquet', 'xlsx'], seats: 20, rows: 5_000_000 },
  unlimited: { tier: 'unlimited', exports: ['csv', 'json', 'ndjson', 'parquet', 'xlsx'], seats: 500, rows: 0 },
}

export const LEGACY_TIER_TO_PLAN: Record<LegacyTier, string> = {
  starter: 'free',
  pro: 'silver',
  gold: 'gold',
  unlimited: 'enterprise',
}

export function legacyExportsFor(tier: LegacyTier): string[] {
  return [...LEGACY_ENTITLEMENTS[tier].exports]
}
