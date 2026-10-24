import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './endpoints'

const asArray = (v) => (Array.isArray(v) ? v : [])

export const keys = {
  me: ['me'],
  bootstrap: ['bootstrap'],
  dashboard: ['dashboard'],
  colonies: ['colonies'],
  users: ['users'],
  accounts: ['accounts'],
  trades: ['trades'],
  approvals: ['approvals'],
  ledger: (colonyId) => ['ledger', colonyId],
}

export const useDashboard = () => useQuery({ queryKey: keys.dashboard, queryFn: api.dashboard })
export const useColonies = () => useQuery({ queryKey: keys.colonies, queryFn: api.colonies, select: asArray })
export const useUsers = () => useQuery({ queryKey: keys.users, queryFn: api.users, select: asArray })
export const useAccounts = () => useQuery({ queryKey: keys.accounts, queryFn: api.accounts, select: asArray })
export const useTrades = () => useQuery({ queryKey: keys.trades, queryFn: api.trades, select: asArray })
export const useApprovals = (enabled = true) =>
  useQuery({ queryKey: keys.approvals, queryFn: api.approvals, select: asArray, enabled })
export const useLedger = (colonyId) =>
  useQuery({ queryKey: keys.ledger(colonyId), queryFn: () => api.ledger(colonyId), select: asArray, enabled: !!colonyId })

// useAction wraps a mutation that changes server state. Everything is
// refetched afterwards: a trade or mint touches balances, ledgers, the
// dashboard and approvals at once, and the data sets are small.
export function useAction(fn, options = {}) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    ...options,
    onSuccess: async (...args) => {
      await qc.invalidateQueries({ predicate: (q) => q.queryKey[0] !== 'me' && q.queryKey[0] !== 'bootstrap' })
      await options.onSuccess?.(...args)
    },
  })
}

// Lookup tables used by most pages to render names instead of IDs.
export function useDirectory() {
  const colonies = useColonies()
  const users = useUsers()
  const colonyById = Object.fromEntries((colonies.data ?? []).map((c) => [c.id, c]))
  const userById = Object.fromEntries((users.data ?? []).map((u) => [u.id, u]))
  return {
    colonies: colonies.data ?? [],
    users: users.data ?? [],
    colonyById,
    userById,
    colonyName: (cid) => colonyById[cid]?.name ?? (cid ? cid.slice(0, 10) : '—'),
    userName: (uid) => userById[uid]?.display_name || userById[uid]?.username || (uid ? uid.slice(0, 10) : '—'),
    isLoading: colonies.isLoading || users.isLoading,
    error: colonies.error || users.error,
  }
}
