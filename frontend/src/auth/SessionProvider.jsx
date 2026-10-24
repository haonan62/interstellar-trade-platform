import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/endpoints'
import { onApiError } from '../api/client'
import { keys } from '../api/queries'

const SessionContext = createContext(null)

// fetchMe returns the signed-in user, or null when there is no session.
async function fetchMe() {
  try {
    return await api.me()
  } catch (err) {
    if (err.status === 401) return null
    throw err
  }
}

export function SessionProvider({ children }) {
  const qc = useQueryClient()
  const [mfaEnrollmentRequired, setMfaEnrollmentRequired] = useState(false)

  const bootstrap = useQuery({ queryKey: keys.bootstrap, queryFn: api.needsBootstrap, staleTime: Infinity })
  const me = useQuery({ queryKey: keys.me, queryFn: fetchMe, staleTime: 5 * 60_000, retry: false })

  useEffect(
    () =>
      onApiError((err) => {
        if (err.status === 401 && qc.getQueryData(keys.me)) {
          // The session expired or was revoked elsewhere.
          qc.setQueryData(keys.me, null)
          qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'me' && q.queryKey[0] !== 'bootstrap' })
        }
        if (err.code === 'mfa_enrollment_required') setMfaEnrollmentRequired(true)
      }),
    [qc],
  )

  const signIn = useCallback(
    async (credentials) => {
      const res = await api.login(credentials)
      setMfaEnrollmentRequired(false)
      qc.setQueryData(keys.me, res.user)
      return res.user
    },
    [qc],
  )

  const initialize = useCallback(
    async (body, bootstrapToken) => {
      const res = await api.bootstrap(body, bootstrapToken)
      qc.setQueryData(keys.bootstrap, { needs_bootstrap: false })
      qc.setQueryData(keys.me, res.user)
      return res.user
    },
    [qc],
  )

  const signOut = useCallback(async () => {
    try {
      await api.logout()
    } catch {
      // The session may already be gone; sign out locally regardless.
    }
    // Set me first: observers are notified and the route guard redirects.
    // qc.clear() would drop the query without telling its observers, leaving
    // the old user on screen.
    await qc.cancelQueries()
    qc.setQueryData(keys.me, null)
    qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'me' && q.queryKey[0] !== 'bootstrap' })
  }, [qc])

  const refreshUser = useCallback(async () => {
    await qc.invalidateQueries({ queryKey: keys.me })
    setMfaEnrollmentRequired(false)
  }, [qc])

  const value = useMemo(
    () => ({
      user: me.data ?? null,
      isLoading: me.isLoading || bootstrap.isLoading,
      error: me.error || bootstrap.error,
      needsBootstrap: !!bootstrap.data?.needs_bootstrap,
      mfaEnrollmentRequired: mfaEnrollmentRequired && !me.data?.mfa_enabled,
      signIn,
      signOut,
      initialize,
      refreshUser,
    }),
    [me.data, me.isLoading, me.error, bootstrap.data, bootstrap.isLoading, bootstrap.error, mfaEnrollmentRequired, signIn, signOut, initialize, refreshUser],
  )

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

// eslint-disable-next-line react-refresh/only-export-components
export function useSession() {
  const ctx = useContext(SessionContext)
  if (!ctx) throw new Error('useSession must be used inside <SessionProvider>')
  return ctx
}
