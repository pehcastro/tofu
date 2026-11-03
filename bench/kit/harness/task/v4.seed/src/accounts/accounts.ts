import type { PlanName } from '../plan/plans'

export type Account = {
  id: string
  name: string
  plan: PlanName
  seatsInUse: number
  active: boolean
  token: string
}

export const ACCOUNTS: readonly Account[] = [
  { id: 'acct_free_1', name: 'Wren Tooling', plan: 'free', seatsInUse: 1, active: true, token: 'tok_wren' },
  { id: 'acct_free_4', name: 'Pell Studio', plan: 'free', seatsInUse: 1, active: false, token: 'tok_pell' },
  { id: 'acct_silver_3', name: 'Marrow Analytics', plan: 'silver', seatsInUse: 4, active: true, token: 'tok_marrow' },
  { id: 'acct_silver_9', name: 'Quill Logistics', plan: 'silver', seatsInUse: 5, active: true, token: 'tok_quill' },
  { id: 'acct_gold_7', name: 'Halcyon Freight', plan: 'gold', seatsInUse: 12, active: true, token: 'tok_halcyon' },
  {
    id: 'acct_gold_70',
    name: 'Halcyon Freight Holdings',
    plan: 'enterprise',
    seatsInUse: 61,
    active: true,
    token: 'tok_halcyon_holdings',
  },
  { id: 'acct_gold_12', name: 'Stroud Rail', plan: 'gold', seatsInUse: 18, active: true, token: 'tok_stroud' },
  { id: 'acct_ent_2', name: 'Northcote Group', plan: 'enterprise', seatsInUse: 140, active: true, token: 'tok_north' },
]
