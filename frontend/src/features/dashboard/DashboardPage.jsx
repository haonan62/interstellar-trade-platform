import { Link as RouterLink } from 'react-router-dom'
import { Box, Button, Card, CardHeader, Divider, Grid, List, ListItem, ListItemText, Typography } from '@mui/material'
import PublicOutlined from '@mui/icons-material/PublicOutlined'
import PeopleOutlined from '@mui/icons-material/PeopleOutlined'
import AccountBalanceWalletOutlined from '@mui/icons-material/AccountBalanceWalletOutlined'
import SwapHorizOutlined from '@mui/icons-material/SwapHorizOutlined'
import TaskAltOutlined from '@mui/icons-material/TaskAltOutlined'
import { useApprovals, useDashboard, useTrades } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { isAdmin } from '../../auth/permissions'
import PageHeader from '../../components/PageHeader'
import StatCard from '../../components/StatCard'
import DataTable from '../../components/DataTable'
import EmptyState from '../../components/EmptyState'
import { TradeStatusChip } from '../../components/Chips'
import { QueryError } from '../../components/QueryState'
import { APPROVAL_TYPE } from '../../lib/constants'
import { formatAmount, formatRelative } from '../../lib/format'

const tradeColumns = [
  { field: 'asset', headerName: 'Asset', flex: 1, minWidth: 120, renderCell: ({ value }) => <strong>{value}</strong> },
  {
    field: 'route',
    headerName: 'Route',
    flex: 1.4,
    minWidth: 160,
    valueGetter: (_, r) => `${r.seller_colony_name} → ${r.buyer_colony_name}`,
  },
  { field: 'price', headerName: 'Price', type: 'number', width: 110, valueFormatter: (v) => formatAmount(v) },
  { field: 'status', headerName: 'Status', width: 190, renderCell: ({ value }) => <TradeStatusChip status={value} /> },
  { field: 'created_at', headerName: 'Created', width: 130, valueFormatter: (v) => formatRelative(v) },
]

const colonyColumns = [
  { field: 'name', headerName: 'Colony', flex: 1, minWidth: 140, valueGetter: (_, r) => r.colony?.name },
  { field: 'accounts', headerName: 'Accounts', type: 'number', width: 100 },
  { field: 'trades_involved', headerName: 'Trades', type: 'number', width: 100 },
]

function ActionItems({ user }) {
  const trades = useTrades()
  const approvals = useApprovals(isAdmin(user))
  const toAccept = (trades.data ?? []).filter((t) => t.status === 'offer_received' && t.buyer_user_id === user?.id)
  const pending = (approvals.data ?? []).filter((a) => a.status === 'pending' && a.proposed_by !== user?.id)
  const items = [
    ...toAccept.map((t) => ({
      key: t.id,
      primary: `Offer for ${t.asset}: ${formatAmount(t.price)}`,
      secondary: `From ${t.seller_name} @ ${t.seller_colony_name}`,
      to: '/trades',
      cta: 'Review',
    })),
    ...pending.map((a) => ({
      key: a.id,
      primary: `${APPROVAL_TYPE[a.type] ?? a.type} awaiting approval`,
      secondary: `Proposed ${formatRelative(a.proposed_at)}`,
      to: '/approvals',
      cta: 'Decide',
    })),
  ]
  return (
    <Card sx={{ height: '100%' }}>
      <CardHeader title="Needs your attention" subheader="Offers to accept and approvals to decide" />
      <Divider />
      {items.length ? (
        <List dense disablePadding>
          {items.slice(0, 6).map((i) => (
            <ListItem
              key={i.key}
              divider
              secondaryAction={
                <Button size="small" component={RouterLink} to={i.to}>
                  {i.cta}
                </Button>
              }
            >
              <ListItemText primary={i.primary} secondary={i.secondary} />
            </ListItem>
          ))}
        </List>
      ) : (
        <EmptyState dense icon={TaskAltOutlined} title="You're all caught up" description="Nothing is waiting on you right now." />
      )}
    </Card>
  )
}

export default function DashboardPage() {
  const { user } = useSession()
  const dash = useDashboard()
  const counts = dash.data?.counts ?? {}
  const name = user?.display_name || user?.username

  return (
    <>
      <PageHeader
        title={`Welcome back${name ? `, ${name}` : ''}`}
        description="Overview of colonies, participants and cross-colony trade activity."
        actions={
          <Button variant="contained" component={RouterLink} to="/trades?new=1" startIcon={<SwapHorizOutlined />}>
            New offer
          </Button>
        }
      />
      <QueryError error={dash.error} onRetry={dash.refetch} />

      <Grid container spacing={2} sx={{ mb: 2 }}>
        {[
          { label: 'Colonies', value: counts.colonies, icon: PublicOutlined },
          { label: 'Users', value: counts.users, icon: PeopleOutlined, tone: 'secondary' },
          { label: 'Funded accounts', value: counts.accounts, icon: AccountBalanceWalletOutlined, tone: 'success' },
          { label: 'Trades', value: counts.trades, icon: SwapHorizOutlined, tone: 'info' },
        ].map((s) => (
          <Grid key={s.label} size={{ xs: 6, lg: 3 }}>
            <StatCard {...s} value={formatAmount(s.value ?? 0)} loading={dash.isLoading} />
          </Grid>
        ))}
      </Grid>

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 8 }}>
          <Card>
            <CardHeader
              title="Recent trades"
              subheader="Latest offers and settlements you can see"
              action={
                <Button size="small" component={RouterLink} to="/trades">
                  View all
                </Button>
              }
            />
            <Divider />
            <DataTable
              rows={dash.data?.recent_trades}
              columns={tradeColumns}
              loading={dash.isLoading}
              pageSize={8}
              empty={{ icon: SwapHorizOutlined, title: 'No trades yet', description: 'Create an offer to start trading between colonies.' }}
            />
          </Card>
        </Grid>
        <Grid size={{ xs: 12, lg: 4 }}>
          <ActionItems user={user} />
        </Grid>
        <Grid size={12}>
          <Card>
            <CardHeader title="Colonies" subheader="Funded accounts and trade involvement per colony" />
            <Divider />
            <DataTable
              rows={dash.data?.colony_summaries}
              columns={colonyColumns}
              getRowId={(r) => r.colony?.id}
              loading={dash.isLoading}
              empty={{
                icon: PublicOutlined,
                title: 'No colonies yet',
                description: 'A super administrator creates colonies before accounts can be funded.',
                action: (
                  <Button variant="outlined" size="small" component={RouterLink} to="/colonies">
                    Go to colonies
                  </Button>
                ),
              }}
            />
          </Card>
        </Grid>
      </Grid>
      <Box sx={{ mt: 2 }}>
        <Typography variant="caption" color="text.secondary">
          Inter-colony messages travel in relay bundles; balances update when bundles are imported.
        </Typography>
      </Box>
    </>
  )
}
