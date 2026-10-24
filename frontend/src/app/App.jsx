import { lazy } from 'react'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Box, CircularProgress, CssBaseline } from '@mui/material'
import { ThemeProvider } from '@mui/material/styles'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SessionProvider, useSession } from '../auth/SessionProvider'
import { canRelay, isAdmin } from '../auth/permissions'
import { NotificationsProvider } from '../components/Notifications'
import ErrorBoundary from '../components/ErrorBoundary'
import { QueryError } from '../components/QueryState'
import AppShell from '../components/layout/AppShell'
import LoginPage from '../features/auth/LoginPage'
import BootstrapPage from '../features/auth/BootstrapPage'
import ResetPasswordPage from '../features/auth/ResetPasswordPage'
import { theme } from './theme'

// Pages are split into their own chunks; the shell and auth screens load first.
const DashboardPage = lazy(() => import('../features/dashboard/DashboardPage'))
const TradesPage = lazy(() => import('../features/trades/TradesPage'))
const AccountsPage = lazy(() => import('../features/accounts/AccountsPage'))
const LedgerPage = lazy(() => import('../features/ledger/LedgerPage'))
const RelayPage = lazy(() => import('../features/relay/RelayPage'))
const ApprovalsPage = lazy(() => import('../features/approvals/ApprovalsPage'))
const ColoniesPage = lazy(() => import('../features/colonies/ColoniesPage'))
const UsersPage = lazy(() => import('../features/users/UsersPage'))
const SecurityPage = lazy(() => import('../features/settings/SecurityPage'))
const NotFoundPage = lazy(() => import('../features/NotFoundPage'))

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      refetchOnWindowFocus: true,
      // Never retry client errors (401/403/404…); retry transient ones twice.
      retry: (count, err) => (err?.status === 0 || err?.status >= 500) && count < 2,
    },
  },
})

function FullPageSpinner() {
  return (
    <Box sx={{ minHeight: '100vh', display: 'grid', placeItems: 'center' }}>
      <CircularProgress size={28} />
    </Box>
  )
}

function RequireAuth({ children }) {
  const { user, isLoading, needsBootstrap } = useSession()
  const location = useLocation()
  if (isLoading) return <FullPageSpinner />
  if (needsBootstrap) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  return children
}

// Guard hides pages the user's roles cannot use (the server still enforces).
function Guard({ allow, children }) {
  const { user } = useSession()
  return allow(user) ? children : <Navigate to="/" replace />
}

function PublicOnly({ children }) {
  const { user, isLoading, needsBootstrap } = useSession()
  if (isLoading) return <FullPageSpinner />
  if (needsBootstrap) return <Navigate to="/setup" replace />
  if (user) return <Navigate to="/" replace />
  return children
}

function SetupOnly({ children }) {
  const { isLoading, needsBootstrap } = useSession()
  if (isLoading) return <FullPageSpinner />
  return needsBootstrap ? children : <Navigate to="/login" replace />
}

function AppRoutes() {
  const { error, isLoading } = useSession()
  if (!isLoading && error && error.status !== 401) {
    // Server unreachable or failing: say so instead of showing a login form.
    return (
      <Box sx={{ maxWidth: 560, mx: 'auto', mt: 12, px: 2 }}>
        <QueryError error={error} onRetry={() => window.location.reload()} />
      </Box>
    )
  }
  return (
    <Routes>
      <Route path="/setup" element={<SetupOnly><BootstrapPage /></SetupOnly>} />
      <Route path="/login" element={<PublicOnly><LoginPage /></PublicOnly>} />
      <Route path="/reset-password" element={<ResetPasswordPage />} />
      <Route element={<RequireAuth><AppShell /></RequireAuth>}>
        <Route index element={<DashboardPage />} />
        <Route path="trades" element={<TradesPage />} />
        <Route path="accounts" element={<AccountsPage />} />
        <Route path="ledger" element={<LedgerPage />} />
        <Route path="relay" element={<Guard allow={canRelay}><RelayPage /></Guard>} />
        <Route path="approvals" element={<Guard allow={isAdmin}><ApprovalsPage /></Guard>} />
        <Route path="colonies" element={<ColoniesPage />} />
        <Route path="users" element={<Guard allow={isAdmin}><UsersPage /></Guard>} />
        <Route path="settings/security" element={<SecurityPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <ThemeProvider theme={theme} defaultMode="system">
      <CssBaseline enableColorScheme />
      <ErrorBoundary>
        <QueryClientProvider client={queryClient}>
          <NotificationsProvider>
            <BrowserRouter>
              <SessionProvider>
                <AppRoutes />
              </SessionProvider>
            </BrowserRouter>
          </NotificationsProvider>
        </QueryClientProvider>
      </ErrorBoundary>
    </ThemeProvider>
  )
}
