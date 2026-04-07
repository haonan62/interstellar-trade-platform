import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AppBar,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Container,
  Divider,
  Drawer,
  FormControl,
  Grid,
  InputLabel,
  List,
  ListItemButton,
  ListItemText,
  MenuItem,
  Paper,
  Select,
  Snackbar,
  Stack,
  TextField,
  Toolbar,
  Typography,
} from '@mui/material'
import { DataGrid } from '@mui/x-data-grid'
import { api } from './api'

const roleOptions = ['super_admin', 'colony_admin', 'trader', 'relay_operator']
const drawerWidth = 260
const sections = ['Dashboard', 'Colonies', 'Users', 'Accounts', 'Trades', 'Relay', 'Ledger']

const asArray = (value) => (Array.isArray(value) ? value : [])

function resolveRowId(row, index = 0) {
  if (row == null || typeof row !== 'object') return index

  const directId = row.id ?? row.user_id ?? row.trade_id ?? row.seq ?? row.hash ?? row.token
  if (directId != null && directId !== '') return directId

  const compositeKeys = ['colony_id', 'username', 'display_name', 'asset', 'created_at', 'time', 'type']
  const compositeId = compositeKeys
    .map((key) => row[key])
    .filter((value) => value != null && value !== '')
    .join('::')

  return compositeId || JSON.stringify(row) || index
}

function formatLabel(value) {
  return String(value)
    .replace(/_/g, ' ')
    .replace(/\b\w/g, (c) => c.toUpperCase())
}

function TextJson({ value }) {
  return (
    <TextField
      value={typeof value === 'string' ? value : JSON.stringify(value, null, 2)}
      fullWidth
      multiline
      minRows={8}
      InputProps={{ readOnly: true }}
    />
  )
}

function EmptyState({ title, description, action }) {
  return (
    <Paper
      variant="outlined"
      sx={{
        p: 4,
        borderStyle: 'dashed',
        borderRadius: 3,
        textAlign: 'center',
        bgcolor: 'background.default',
      }}
    >
      <Stack spacing={1.5} alignItems="center">
        <Typography variant="h6">{title}</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ maxWidth: 560 }}>
          {description}
        </Typography>
        {action}
      </Stack>
    </Paper>
  )
}

function SectionCard({ title, subtitle, children, action }) {
  return (
    <Card sx={{ height: '100%' }}>
      <CardContent>
        <Stack spacing={2}>
          <Stack direction={{ xs: 'column', sm: 'row' }} justifyContent="space-between" spacing={1}>
            <Box>
              <Typography variant="h6">{title}</Typography>
              {subtitle ? (
                <Typography variant="body2" color="text.secondary">
                  {subtitle}
                </Typography>
              ) : null}
            </Box>
            {action}
          </Stack>
          {children}
        </Stack>
      </CardContent>
    </Card>
  )
}

function DataSection({ columns, rows, actions, emptyState, pageSize = 5, height = 420 }) {
  const safeRows = asArray(rows)
  const actionColumn =
    typeof actions === 'function'
      ? [
        {
          field: '__actions__',
          headerName: 'Actions',
          sortable: false,
          filterable: false,
          width: 140,
          renderCell: (params) => actions(params.row),
        },
      ]
      : []

  if (!safeRows.length) return emptyState

  return (
    <Box sx={{ width: '100%' }}>
      <DataGrid
        autoHeight={false}
        rows={safeRows}
        getRowId={(row) => resolveRowId(row, safeRows.indexOf(row))}
        columns={[...columns, ...actionColumn]}
        disableRowSelectionOnClick
        pageSizeOptions={[5, 10, 25]}
        initialState={{
          pagination: { paginationModel: { pageSize, page: 0 } },
        }}
        sx={{
          border: 0,
          minHeight: height,
          '& .MuiDataGrid-columnHeaders': {
            bgcolor: 'grey.50',
            borderRadius: 2,
          },
          '& .MuiDataGrid-cell:focus, & .MuiDataGrid-columnHeader:focus': {
            outline: 'none',
          },
        }}
      />
    </Box>
  )
}

export default function App() {
  const [token, setToken] = useState(localStorage.getItem('itp_token') || '')
  const [needsBootstrap, setNeedsBootstrap] = useState(true)
  const [user, setUser] = useState(null)
  const [section, setSection] = useState('Dashboard')
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const [messageSeverity, setMessageSeverity] = useState('info')
  const [dashboard, setDashboard] = useState(null)
  const [colonies, setColonies] = useState([])
  const [users, setUsers] = useState([])
  const [accounts, setAccounts] = useState([])
  const [trades, setTrades] = useState([])
  const [ledger, setLedger] = useState([])
  const [selectedLedgerColony, setSelectedLedgerColony] = useState('')
  const [exportedBundle, setExportedBundle] = useState('')
  const [importBundleText, setImportBundleText] = useState('')

  const [bootstrapForm, setBootstrapForm] = useState({ username: 'admin', display_name: 'Administrator', password: 'ChangeMe123' })
  const [loginForm, setLoginForm] = useState({ username: 'admin', password: 'ChangeMe123' })
  const [colonyForm, setColonyForm] = useState({ name: '' })
  const [trustForm, setTrustForm] = useState({ colony_id: '', peer_colony_id: '' })
  const [userForm, setUserForm] = useState({ username: '', display_name: '', password: 'Password123', colony_id: '', roles: ['trader'] })
  const [mintForm, setMintForm] = useState({ colony_id: '', user_id: '', amount: 1000 })
  const [offerForm, setOfferForm] = useState({ seller_user_id: '', buyer_user_id: '', asset: 'design-v1', price: 150 })
  const [relayExportForm, setRelayExportForm] = useState({ colony_id: '', to_colony_id: '' })
  const [relayImportForm, setRelayImportForm] = useState({ colony_id: '' })

  const byColonyUsers = useMemo(() => users.filter((u) => !!u.colony_id), [users])
  const myColonyUsers = useMemo(() => users.filter((u) => u.colony_id === user?.colony_id), [users, user])
  const hasRole = (role) => user?.roles?.includes(role)

  const notify = (text, severity = 'info') => {
    setMessage(text)
    setMessageSeverity(severity)
  }

  const refresh = async (activeToken = token) => {
    if (!activeToken) return
    setLoading(true)
    try {
      const [me, dash, c, u, a, t] = await Promise.all([
        api.me(activeToken),
        api.dashboard(activeToken),
        api.colonies(activeToken),
        api.users(activeToken),
        api.accounts(activeToken),
        api.trades(activeToken),
      ])
      setUser(me)
      setDashboard({
        ...dash,
        counts: dash?.counts || {},
        colony_summaries: asArray(dash?.colony_summaries),
        recent_trades: asArray(dash?.recent_trades),
      })
      setColonies(asArray(c))
      setUsers(asArray(u))
      setAccounts(asArray(a))
      setTrades(asArray(t))
      const defaultColony = asArray(c)[0]?.id || ''
      if (!selectedLedgerColony && defaultColony) setSelectedLedgerColony(defaultColony)
    } catch (err) {
      notify(err.message, 'error')
      if (String(err.message).toLowerCase().includes('session')) {
        logout()
      }
    } finally {
      setLoading(false)
    }
  }

  const refreshLedger = async (colonyId = selectedLedgerColony) => {
    if (!token || !colonyId) return
    try {
      const entries = await api.ledger(token, colonyId)
      setLedger(asArray(entries))
    } catch (err) {
      notify(err.message, 'error')
    }
  }

  useEffect(() => {
    api.needsBootstrap().then((v) => setNeedsBootstrap(v.needs_bootstrap)).catch((err) => notify(err.message, 'error'))
  }, [])

  useEffect(() => {
    if (token) {
      localStorage.setItem('itp_token', token)
      refresh(token)
    } else {
      localStorage.removeItem('itp_token')
      setUser(null)
    }
  }, [token])

  useEffect(() => {
    if (selectedLedgerColony) refreshLedger(selectedLedgerColony)
  }, [selectedLedgerColony])

  const logout = async () => {
    try {
      if (token) await api.logout(token)
    } catch { }
    setToken('')
    setUser(null)
    setDashboard(null)
    setColonies([])
    setUsers([])
    setAccounts([])
    setTrades([])
    setLedger([])
  }

  const submit = async (fn, successMessage = 'Saved successfully') => {
    setLoading(true)
    try {
      await fn()
      await refresh()
      if (selectedLedgerColony) await refreshLedger(selectedLedgerColony)
      notify(successMessage, 'success')
    } catch (err) {
      notify(err.message, 'error')
    } finally {
      setLoading(false)
    }
  }

  const canAdmin = hasRole('super_admin') || hasRole('colony_admin')
  const canRelay = hasRole('super_admin') || hasRole('relay_operator') || hasRole('colony_admin')

  const colonyRows = useMemo(
    () =>
      colonies.map((colony) => ({
        ...colony,
        trustedNames: Object.keys(colony.trusted_colonies || {}).join(', ') || '—',
        claimsText: JSON.stringify(colony.net_claims || {}),
        obligationsText: JSON.stringify(colony.net_obligations || {}),
      })),
    [colonies],
  )

  const userRows = useMemo(
    () =>
      users.map((entry) => ({
        ...entry,
        roleNames: (entry.roles || []).join(', '),
      })),
    [users],
  )

  const tradeRows = useMemo(() => trades.map((trade) => ({ ...trade })), [trades])

  const dashboardColonyRows = useMemo(
    () =>
      asArray(dashboard?.colony_summaries).map((summary) => ({
        ...summary,
        id: summary.colony.id,
        name: summary.colony.name,
        claimsText: JSON.stringify(summary.colony.net_claims || {}),
        obligationsText: JSON.stringify(summary.colony.net_obligations || {}),
      })),
    [dashboard],
  )

  const dashboardTradeRows = useMemo(
    () =>
      asArray(dashboard?.recent_trades).map((trade) => ({
        ...trade,
      })),
    [dashboard],
  )

  const colonyColumns = [
    { field: 'name', headerName: 'Colony', flex: 1.1, minWidth: 180 },
    { field: 'id', headerName: 'ID', flex: 1.3, minWidth: 220 },
    { field: 'trustedNames', headerName: 'Trusted Peers', flex: 1.1, minWidth: 180 },
    { field: 'claimsText', headerName: 'Claims', flex: 1, minWidth: 180 },
    { field: 'obligationsText', headerName: 'Obligations', flex: 1, minWidth: 180 },
  ]

  const userColumns = [
    { field: 'username', headerName: 'Username', flex: 1, minWidth: 140 },
    { field: 'display_name', headerName: 'Display Name', flex: 1.1, minWidth: 160 },
    { field: 'colony_id', headerName: 'Colony', flex: 1.2, minWidth: 180, valueGetter: (_, row) => row.colony_id || 'Unassigned' },
    {
      field: 'roleNames',
      headerName: 'Roles',
      flex: 1.3,
      minWidth: 220,
      renderCell: (params) => (
        <Stack direction="row" spacing={0.75} useFlexGap flexWrap="wrap" sx={{ py: 1 }}>
          {(params.row.roles || []).map((role) => (
            <Chip key={role} size="small" label={formatLabel(role)} variant="outlined" />
          ))}
        </Stack>
      ),
    },
  ]

  const accountColumns = [
    { field: 'username', headerName: 'Username', flex: 1, minWidth: 150 },
    { field: 'colony_name', headerName: 'Colony', flex: 1, minWidth: 160 },
    { field: 'balance', headerName: 'Balance', flex: 0.7, minWidth: 120, type: 'number' },
  ]

  const tradeColumns = [
    { field: 'id', headerName: 'Trade ID', flex: 1.1, minWidth: 220 },
    { field: 'asset', headerName: 'Asset', flex: 0.9, minWidth: 140 },
    { field: 'price', headerName: 'Price', flex: 0.6, minWidth: 110, type: 'number' },
    {
      field: 'seller',
      headerName: 'Seller',
      flex: 1.1,
      minWidth: 180,
      valueGetter: (_, row) => `${row.seller_name} (${row.seller_colony_name})`,
    },
    {
      field: 'buyer',
      headerName: 'Buyer',
      flex: 1.1,
      minWidth: 180,
      valueGetter: (_, row) => `${row.buyer_name} (${row.buyer_colony_name})`,
    },
    {
      field: 'status',
      headerName: 'Status',
      flex: 0.8,
      minWidth: 150,
      renderCell: (params) => <Chip size="small" label={formatLabel(params.value)} color={params.value === 'settled' ? 'success' : 'default'} />,
    },
  ]

  const dashboardColonyColumns = [
    { field: 'name', headerName: 'Colony', flex: 1.1, minWidth: 180 },
    { field: 'accounts', headerName: 'Accounts', flex: 0.6, minWidth: 120, type: 'number' },
    { field: 'trades_involved', headerName: 'Trades Involved', flex: 0.7, minWidth: 140, type: 'number' },
    { field: 'claimsText', headerName: 'Claims', flex: 1, minWidth: 180 },
    { field: 'obligationsText', headerName: 'Obligations', flex: 1, minWidth: 180 },
  ]

  const dashboardTradeColumns = [
    { field: 'asset', headerName: 'Asset', flex: 1, minWidth: 140 },
    { field: 'price', headerName: 'Price', flex: 0.6, minWidth: 100, type: 'number' },
    {
      field: 'counterparties',
      headerName: 'Route',
      flex: 1.3,
      minWidth: 220,
      valueGetter: (_, row) => `${row.seller_colony_name} → ${row.buyer_colony_name}`,
    },
    {
      field: 'status',
      headerName: 'Status',
      flex: 0.8,
      minWidth: 150,
      renderCell: (params) => <Chip size="small" label={formatLabel(params.value)} variant="outlined" />,
    },
  ]

  if (needsBootstrap) {
    return (
      <Container maxWidth="sm" sx={{ py: 10 }}>
        <Card sx={{ borderRadius: 4 }}>
          <CardContent sx={{ p: 4 }}>
            <Stack spacing={3}>
              <Box>
                <Typography variant="overline" color="primary.main">
                  Interstellar Trade Platform
                </Typography>
                <Typography variant="h4" sx={{ mt: 0.5 }}>
                  Initialize workspace
                </Typography>
                <Typography variant="body1" color="text.secondary" sx={{ mt: 1 }}>
                  Set up the first administrator to activate colony operations, relay controls, and trading workflows.
                </Typography>
              </Box>
              <TextField label="Username" value={bootstrapForm.username} onChange={(e) => setBootstrapForm({ ...bootstrapForm, username: e.target.value })} />
              <TextField label="Display name" value={bootstrapForm.display_name} onChange={(e) => setBootstrapForm({ ...bootstrapForm, display_name: e.target.value })} />
              <TextField label="Password" type="password" value={bootstrapForm.password} onChange={(e) => setBootstrapForm({ ...bootstrapForm, password: e.target.value })} helperText="Use a strong password for the first administrative account." />
              <Button
                variant="contained"
                size="large"
                disabled={loading}
                onClick={async () => {
                  try {
                    const res = await api.bootstrap(bootstrapForm)
                    setNeedsBootstrap(false)
                    setToken(res.token)
                    notify('Workspace initialized successfully', 'success')
                  } catch (err) {
                    notify(err.message, 'error')
                  }
                }}
              >
                Create administrator
              </Button>
            </Stack>
          </CardContent>
        </Card>
        <Snackbar open={!!message} autoHideDuration={4000} onClose={() => setMessage('')}>
          <Alert severity={messageSeverity}>{message}</Alert>
        </Snackbar>
      </Container>
    )
  }

  if (!token) {
    return (
      <Container maxWidth="sm" sx={{ py: 10 }}>
        <Card sx={{ borderRadius: 4 }}>
          <CardContent sx={{ p: 4 }}>
            <Stack spacing={3}>
              <Box>
                <Typography variant="overline" color="primary.main">
                  Interstellar Trade Platform
                </Typography>
                <Typography variant="h4" sx={{ mt: 0.5 }}>
                  Sign in to mission control
                </Typography>
                <Typography variant="body1" color="text.secondary" sx={{ mt: 1 }}>
                  Access colony administration, trading operations, and relay processing from one workspace.
                </Typography>
              </Box>
              <TextField label="Username" value={loginForm.username} onChange={(e) => setLoginForm({ ...loginForm, username: e.target.value })} />
              <TextField label="Password" type="password" value={loginForm.password} onChange={(e) => setLoginForm({ ...loginForm, password: e.target.value })} />
              <Button
                variant="contained"
                size="large"
                disabled={loading}
                onClick={async () => {
                  try {
                    const res = await api.login(loginForm)
                    setToken(res.token)
                    notify('Signed in successfully', 'success')
                  } catch (err) {
                    notify(err.message, 'error')
                  }
                }}
              >
                Sign in
              </Button>
            </Stack>
          </CardContent>
        </Card>
        <Snackbar open={!!message} autoHideDuration={4000} onClose={() => setMessage('')}>
          <Alert severity={messageSeverity}>{message}</Alert>
        </Snackbar>
      </Container>
    )
  }

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh', bgcolor: 'background.default' }}>
      <AppBar
        position="fixed"
        color="inherit"
        elevation={0}
        sx={{
          width: { md: `calc(100% - ${drawerWidth}px)` },
          ml: { md: `${drawerWidth}px` },
          borderBottom: 1,
          borderColor: 'divider',
          bgcolor: 'rgba(255,255,255,0.9)',
          backdropFilter: 'blur(10px)',
        }}
      >
        <Toolbar sx={{ gap: 2 }}>
          <Box sx={{ flexGrow: 1 }}>
            <Typography variant="h6">{section}</Typography>
            <Typography variant="body2" color="text.secondary">
              Manage interstellar trading workflows with a cleaner operational workspace.
            </Typography>
          </Box>
          <Chip label={user?.display_name || user?.username} color="primary" variant="outlined" />
          <Chip label={(user?.roles || []).map(formatLabel).join(' · ')} variant="outlined" />
          <Button variant="outlined" onClick={refresh} disabled={loading}>
            Refresh
          </Button>
          <Button variant="contained" onClick={logout}>
            Logout
          </Button>
        </Toolbar>
      </AppBar>

      <Drawer
        variant="permanent"
        sx={{
          width: drawerWidth,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width: drawerWidth,
            boxSizing: 'border-box',
            borderRight: 1,
            borderColor: 'divider',
            bgcolor: 'background.paper',
          },
        }}
      >
        <Toolbar>
          <Stack spacing={0.5}>
            <Typography variant="overline" color="primary.main">
              Singularity Console
            </Typography>
            <Typography variant="h6">Interstellar Trade</Typography>
          </Stack>
        </Toolbar>
        <Divider />
        <Box sx={{ px: 2, py: 2 }}>
          <Paper variant="outlined" sx={{ p: 2, borderRadius: 3, bgcolor: 'primary.50' }}>
            <Typography variant="subtitle2">Mission status</Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
              {loading ? 'Refreshing live data…' : 'Connected and ready for colony operations.'}
            </Typography>
          </Paper>
        </Box>
        <List sx={{ px: 1.5 }}>
          {sections.map((name) => (
            <ListItemButton
              key={name}
              selected={section === name}
              onClick={() => setSection(name)}
              sx={{ borderRadius: 2, mb: 0.5 }}
            >
              <ListItemText
                primary={name}
                secondary={name === 'Dashboard' ? 'Overview and activity' : undefined}
                primaryTypographyProps={{ fontWeight: section === name ? 700 : 500 }}
              />
            </ListItemButton>
          ))}
        </List>
      </Drawer>

      <Box component="main" sx={{ flexGrow: 1, minWidth: 0 }}>
        <Toolbar />
        <Container maxWidth="xl" sx={{ py: 4 }}>
          {section === 'Dashboard' && dashboard && (
            <Stack spacing={3}>
              <Grid container spacing={2}>
                {Object.entries(dashboard?.counts ?? {}).map(([key, value]) => (
                  <Grid item xs={12} sm={6} lg={3} key={key}>
                    <Card sx={{ height: '100%' }}>
                      <CardContent>
                        <Typography variant="overline" color="text.secondary">
                          {formatLabel(key)}
                        </Typography>
                        <Typography variant="h4" sx={{ mt: 1, fontWeight: 700 }}>
                          {value}
                        </Typography>
                      </CardContent>
                    </Card>
                  </Grid>
                ))}
              </Grid>

              <Grid container spacing={2}>
                <Grid item xs={12} lg={7}>
                  <SectionCard
                    title="Colony summaries"
                    subtitle="Monitor account distribution, trade involvement, and inter-colony obligations at a glance."
                  >
                    <DataSection
                      columns={dashboardColonyColumns}
                      rows={dashboardColonyRows}
                      pageSize={5}
                      emptyState={
                        <EmptyState
                          title="No colonies yet"
                          description="Create your first colony to activate account issuance and interstellar routing."
                          action={<Button variant="contained" onClick={() => setSection('Colonies')}>Create colony</Button>}
                        />
                      }
                    />
                  </SectionCard>
                </Grid>
                <Grid item xs={12} lg={5}>
                  <SectionCard
                    title="Recent trades"
                    subtitle="Review the latest cross-colony exchanges and their settlement status."
                  >
                    <DataSection
                      columns={dashboardTradeColumns}
                      rows={dashboardTradeRows}
                      pageSize={5}
                      emptyState={
                        <EmptyState
                          title="No trade activity yet"
                          description="Create an offer to start settlement and relay activity across colonies."
                          action={<Button variant="outlined" onClick={() => setSection('Trades')}>Open trades</Button>}
                        />
                      }
                    />
                  </SectionCard>
                </Grid>
              </Grid>
            </Stack>
          )}

          {section === 'Colonies' && (
            <Grid container spacing={2}>
              <Grid item xs={12} lg={8}>
                <SectionCard title="Colonies" subtitle="Manage colony identities, trust links, and financial exposure.">
                  <DataSection
                    columns={colonyColumns}
                    rows={colonyRows}
                    emptyState={
                      <EmptyState
                        title="No colonies configured"
                        description="Create a colony to begin assigning users, minting balances, and establishing trust relationships."
                      />
                    }
                  />
                </SectionCard>
              </Grid>
              <Grid item xs={12} lg={4}>
                <Stack spacing={2}>
                  <SectionCard title="Create colony">
                    <Stack spacing={2}>
                      <TextField label="Colony name" value={colonyForm.name} onChange={(e) => setColonyForm({ name: e.target.value })} />
                      <Button
                        variant="contained"
                        disabled={!hasRole('super_admin') || loading || !colonyForm.name}
                        onClick={() =>
                          submit(async () => {
                            await api.createColony(token, colonyForm)
                            setColonyForm({ name: '' })
                          }, 'Colony created successfully')
                        }
                      >
                        Create colony
                      </Button>
                    </Stack>
                  </SectionCard>
                  <SectionCard title="Authorize trading partner">
                    <Stack spacing={2}>
                      <FormControl fullWidth>
                        <InputLabel>Colony</InputLabel>
                        <Select value={trustForm.colony_id} label="Colony" onChange={(e) => setTrustForm({ ...trustForm, colony_id: e.target.value })}>
                          {colonies.map((c) => (
                            <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                          ))}
                        </Select>
                      </FormControl>
                      <FormControl fullWidth>
                        <InputLabel>Peer colony</InputLabel>
                        <Select value={trustForm.peer_colony_id} label="Peer colony" onChange={(e) => setTrustForm({ ...trustForm, peer_colony_id: e.target.value })}>
                          {colonies.map((c) => (
                            <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                          ))}
                        </Select>
                      </FormControl>
                      <Button
                        variant="contained"
                        disabled={!canAdmin || loading || !trustForm.colony_id || !trustForm.peer_colony_id}
                        onClick={() =>
                          submit(
                            async () => {
                              await api.trustPeer(token, trustForm.colony_id, { peer_colony_id: trustForm.peer_colony_id })
                            },
                            'Trading partner authorized successfully',
                          )
                        }
                      >
                        Save trust link
                      </Button>
                    </Stack>
                  </SectionCard>
                </Stack>
              </Grid>
            </Grid>
          )}

          {section === 'Users' && (
            <Grid container spacing={2}>
              <Grid item xs={12} lg={8}>
                <SectionCard title="Users" subtitle="Provision operators, colony administrators, and traders with role-based access.">
                  <DataSection
                    columns={userColumns}
                    rows={userRows}
                    emptyState={
                      <EmptyState
                        title="No users available"
                        description="Create a user to assign operational access and colony responsibilities."
                      />
                    }
                  />
                </SectionCard>
              </Grid>
              <Grid item xs={12} lg={4}>
                <SectionCard title="Create user">
                  <Stack spacing={2}>
                    <TextField label="Username" value={userForm.username} onChange={(e) => setUserForm({ ...userForm, username: e.target.value })} />
                    <TextField label="Display name" value={userForm.display_name} onChange={(e) => setUserForm({ ...userForm, display_name: e.target.value })} />
                    <TextField label="Password" value={userForm.password} onChange={(e) => setUserForm({ ...userForm, password: e.target.value })} />
                    <FormControl fullWidth>
                      <InputLabel>Colony</InputLabel>
                      <Select value={userForm.colony_id} label="Colony" onChange={(e) => setUserForm({ ...userForm, colony_id: e.target.value })}>
                        {colonies.map((c) => (
                          <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <FormControl fullWidth>
                      <InputLabel>Roles</InputLabel>
                      <Select multiple value={userForm.roles} label="Roles" onChange={(e) => setUserForm({ ...userForm, roles: e.target.value })}>
                        {roleOptions.map((role) => (
                          <MenuItem key={role} value={role}>{formatLabel(role)}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <Button
                      variant="contained"
                      disabled={!canAdmin || loading || !userForm.username || !userForm.display_name}
                      onClick={() =>
                        submit(async () => {
                          await api.createUser(token, userForm)
                          setUserForm({ ...userForm, username: '', display_name: '' })
                        }, 'User created successfully')
                      }
                    >
                      Create user
                    </Button>
                  </Stack>
                </SectionCard>
              </Grid>
            </Grid>
          )}

          {section === 'Accounts' && (
            <Grid container spacing={2}>
              <Grid item xs={12} lg={8}>
                <SectionCard title="Accounts" subtitle="Review balances issued across colonies and participants.">
                  <DataSection
                    columns={accountColumns}
                    rows={accounts}
                    emptyState={
                      <EmptyState
                        title="No accounts funded yet"
                        description="Issue a starting balance to a user to activate trading and settlement workflows."
                      />
                    }
                  />
                </SectionCard>
              </Grid>
              <Grid item xs={12} lg={4}>
                <SectionCard title="Issue starting balance">
                  <Stack spacing={2}>
                    <FormControl fullWidth>
                      <InputLabel>Colony</InputLabel>
                      <Select value={mintForm.colony_id} label="Colony" onChange={(e) => setMintForm({ ...mintForm, colony_id: e.target.value, user_id: '' })}>
                        {colonies.map((c) => (
                          <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <FormControl fullWidth>
                      <InputLabel>User</InputLabel>
                      <Select value={mintForm.user_id} label="User" onChange={(e) => setMintForm({ ...mintForm, user_id: e.target.value })}>
                        {users.filter((u) => u.colony_id === mintForm.colony_id).map((u) => (
                          <MenuItem key={u.id} value={u.id}>{u.display_name}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <TextField type="number" label="Amount" value={mintForm.amount} onChange={(e) => setMintForm({ ...mintForm, amount: Number(e.target.value) })} />
                    <Button
                      variant="contained"
                      disabled={!canAdmin || loading || !mintForm.colony_id || !mintForm.user_id}
                      onClick={() => submit(async () => { await api.mint(token, mintForm) }, 'Funds issued successfully')}
                    >
                      Issue balance
                    </Button>
                  </Stack>
                </SectionCard>
              </Grid>
            </Grid>
          )}

          {section === 'Trades' && (
            <Grid container spacing={2}>
              <Grid item xs={12} lg={8}>
                <SectionCard title="Trades" subtitle="Track offer flow, acceptance, and settlement between colonies.">
                  <DataSection
                    columns={tradeColumns}
                    rows={tradeRows}
                    actions={(row) =>
                      row.status === 'offer_received' && row.buyer_user_id === user?.id ? (
                        <Button size="small" variant="outlined" onClick={() => submit(async () => { await api.acceptTrade(token, row.id) }, 'Trade accepted successfully')}>
                          Accept
                        </Button>
                      ) : null
                    }
                    emptyState={
                      <EmptyState
                        title="No trades yet"
                        description="Create an offer to begin exchange, settlement, and relay processing."
                      />
                    }
                  />
                </SectionCard>
              </Grid>
              <Grid item xs={12} lg={4}>
                <SectionCard title="Create offer">
                  <Stack spacing={2}>
                    <FormControl fullWidth>
                      <InputLabel>Seller</InputLabel>
                      <Select value={offerForm.seller_user_id} label="Seller" onChange={(e) => setOfferForm({ ...offerForm, seller_user_id: e.target.value })}>
                        {(hasRole('super_admin') ? byColonyUsers : myColonyUsers).map((u) => (
                          <MenuItem key={u.id} value={u.id}>{u.display_name}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <FormControl fullWidth>
                      <InputLabel>Buyer</InputLabel>
                      <Select value={offerForm.buyer_user_id} label="Buyer" onChange={(e) => setOfferForm({ ...offerForm, buyer_user_id: e.target.value })}>
                        {byColonyUsers.filter((u) => u.id !== offerForm.seller_user_id).map((u) => (
                          <MenuItem key={u.id} value={u.id}>{u.display_name}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <TextField label="Asset" value={offerForm.asset} onChange={(e) => setOfferForm({ ...offerForm, asset: e.target.value })} />
                    <TextField type="number" label="Price" value={offerForm.price} onChange={(e) => setOfferForm({ ...offerForm, price: Number(e.target.value) })} />
                    <Button
                      variant="contained"
                      disabled={loading || !offerForm.seller_user_id || !offerForm.buyer_user_id || !offerForm.asset}
                      onClick={() => submit(async () => { await api.createOffer(token, offerForm) }, 'Trade offer created successfully')}
                    >
                      Create offer
                    </Button>
                  </Stack>
                </SectionCard>
              </Grid>
            </Grid>
          )}

          {section === 'Relay' && (
            <Grid container spacing={2}>
              <Grid item xs={12} lg={6}>
                <SectionCard title="Generate outbound relay package" subtitle="Export messages that need to move between colonies.">
                  <Stack spacing={2}>
                    <FormControl fullWidth>
                      <InputLabel>From colony</InputLabel>
                      <Select value={relayExportForm.colony_id} label="From colony" onChange={(e) => setRelayExportForm({ ...relayExportForm, colony_id: e.target.value })}>
                        {colonies.map((c) => (
                          <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <FormControl fullWidth>
                      <InputLabel>To colony</InputLabel>
                      <Select value={relayExportForm.to_colony_id} label="To colony" onChange={(e) => setRelayExportForm({ ...relayExportForm, to_colony_id: e.target.value })}>
                        {colonies.map((c) => (
                          <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <Button
                      variant="contained"
                      disabled={!canRelay || loading || !relayExportForm.colony_id || !relayExportForm.to_colony_id}
                      onClick={async () => {
                        try {
                          const bundle = await api.exportBundle(token, relayExportForm)
                          setExportedBundle(JSON.stringify(bundle, null, 2))
                          notify(`Exported ${bundle.messages.length} message(s)`, 'success')
                        } catch (err) {
                          notify(err.message, 'error')
                        }
                      }}
                    >
                      Generate bundle
                    </Button>
                    <TextJson value={exportedBundle || '{}'} />
                  </Stack>
                </SectionCard>
              </Grid>
              <Grid item xs={12} lg={6}>
                <SectionCard title="Process inbound relay package" subtitle="Apply exported messages into the selected target colony.">
                  <Stack spacing={2}>
                    <FormControl fullWidth>
                      <InputLabel>Target colony</InputLabel>
                      <Select value={relayImportForm.colony_id} label="Target colony" onChange={(e) => setRelayImportForm({ ...relayImportForm, colony_id: e.target.value })}>
                        {colonies.map((c) => (
                          <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                    <TextField multiline minRows={14} label="Bundle JSON" value={importBundleText} onChange={(e) => setImportBundleText(e.target.value)} />
                    <Button
                      variant="contained"
                      disabled={!canRelay || loading || !relayImportForm.colony_id || !importBundleText}
                      onClick={() =>
                        submit(async () => {
                          const bundle = JSON.parse(importBundleText)
                          const res = await api.importBundle(token, { colony_id: relayImportForm.colony_id, bundle })
                          notify(`Imported ${res.imported_count} message(s)`, 'success')
                        }, 'Relay package processed successfully')
                      }
                    >
                      Process bundle
                    </Button>
                  </Stack>
                </SectionCard>
              </Grid>
            </Grid>
          )}

          {section === 'Ledger' && (
            <SectionCard title="Ledger" subtitle="Inspect raw ledger activity for a selected colony.">
              <Stack spacing={2}>
                <FormControl sx={{ maxWidth: 320 }}>
                  <InputLabel>Colony</InputLabel>
                  <Select value={selectedLedgerColony} label="Colony" onChange={(e) => setSelectedLedgerColony(e.target.value)}>
                    {colonies.map((c) => (
                      <MenuItem key={c.id} value={c.id}>{c.name}</MenuItem>
                    ))}
                  </Select>
                </FormControl>
                <Button sx={{ maxWidth: 180 }} variant="outlined" onClick={() => refreshLedger()}>
                  Reload ledger
                </Button>
                {ledger.length ? (
                  <TextJson value={ledger} />
                ) : (
                  <EmptyState
                    title="No ledger entries"
                    description="Once balances are issued or trades settle, ledger events will appear here for the selected colony."
                  />
                )}
              </Stack>
            </SectionCard>
          )}
        </Container>
      </Box>
      <Snackbar open={!!message} autoHideDuration={4000} onClose={() => setMessage('')}>
        <Alert severity={messageSeverity}>{message}</Alert>
      </Snackbar>
    </Box>
  )
}
