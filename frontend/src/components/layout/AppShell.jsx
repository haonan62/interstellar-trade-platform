import { Suspense, useState } from 'react'
import { Outlet, Link as RouterLink, useLocation } from 'react-router-dom'
import { Alert, AppBar, Box, Button, Drawer, IconButton, LinearProgress, Toolbar, Tooltip } from '@mui/material'
import MenuOutlined from '@mui/icons-material/MenuOutlined'
import RefreshOutlined from '@mui/icons-material/RefreshOutlined'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { useSession } from '../../auth/SessionProvider'
import ErrorBoundary from '../ErrorBoundary'
import Sidebar from './Sidebar'
import UserMenu from './UserMenu'
import ColorModeToggle from './ColorModeToggle'

const DRAWER_WIDTH = 248

export default function AppShell() {
  const [mobileOpen, setMobileOpen] = useState(false)
  const { mfaEnrollmentRequired } = useSession()
  const fetching = useIsFetching()
  const qc = useQueryClient()
  const location = useLocation()

  const drawerPaper = { width: DRAWER_WIDTH, boxSizing: 'border-box', borderRight: 1, borderColor: 'divider', bgcolor: 'background.paper' }

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh', bgcolor: 'background.default' }}>
      <Box component="aside" sx={{ width: { lg: DRAWER_WIDTH }, flexShrink: { lg: 0 } }}>
        <Drawer
          variant="temporary"
          open={mobileOpen}
          onClose={() => setMobileOpen(false)}
          ModalProps={{ keepMounted: true }}
          sx={{ display: { xs: 'block', lg: 'none' }, '& .MuiDrawer-paper': drawerPaper }}
        >
          <Sidebar onNavigate={() => setMobileOpen(false)} />
        </Drawer>
        <Drawer variant="permanent" open sx={{ display: { xs: 'none', lg: 'block' }, '& .MuiDrawer-paper': drawerPaper }}>
          <Sidebar />
        </Drawer>
      </Box>

      <Box sx={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
        <AppBar
          position="sticky"
          color="inherit"
          elevation={0}
          sx={{ borderBottom: 1, borderColor: 'divider', bgcolor: 'background.paper' }}
        >
          <Toolbar sx={{ gap: 1, minHeight: { xs: 64 } }}>
            <IconButton edge="start" onClick={() => setMobileOpen(true)} sx={{ display: { lg: 'none' } }} aria-label="Open navigation">
              <MenuOutlined />
            </IconButton>
            <Box sx={{ flex: 1 }} />
            <Tooltip title="Refresh data">
              <IconButton onClick={() => qc.invalidateQueries()} aria-label="Refresh data">
                <RefreshOutlined fontSize="small" />
              </IconButton>
            </Tooltip>
            <ColorModeToggle />
            <UserMenu />
          </Toolbar>
          <LinearProgress sx={{ height: 2, visibility: fetching ? 'visible' : 'hidden', position: 'absolute', bottom: 0, left: 0, right: 0 }} />
        </AppBar>

        <Box component="main" sx={{ flex: 1, px: { xs: 2, md: 4 }, py: { xs: 3, md: 4 }, maxWidth: 1440, width: '100%', mx: 'auto' }}>
          {mfaEnrollmentRequired && location.pathname !== '/settings/security' && (
            <Alert
              severity="warning"
              sx={{ mb: 3 }}
              action={
                <Button component={RouterLink} to="/settings/security" color="inherit" size="small">
                  Set up MFA
                </Button>
              }
            >
              Administrators must enable two-factor authentication before using the platform.
            </Alert>
          )}
          <ErrorBoundary key={location.pathname}>
            <Suspense fallback={<LinearProgress />}>
              <Outlet />
            </Suspense>
          </ErrorBoundary>
        </Box>
      </Box>
    </Box>
  )
}
