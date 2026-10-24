import { useState } from 'react'
import { Box, Button, Card, Grid } from '@mui/material'
import AddOutlined from '@mui/icons-material/AddOutlined'
import AccountBalanceWalletOutlined from '@mui/icons-material/AccountBalanceWalletOutlined'
import { useAccounts } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { isAdmin } from '../../auth/permissions'
import PageHeader from '../../components/PageHeader'
import DataTable from '../../components/DataTable'
import StatCard from '../../components/StatCard'
import { QueryError } from '../../components/QueryState'
import { formatAmount } from '../../lib/format'
import MintDialog from './MintDialog'

export default function AccountsPage() {
  const { user } = useSession()
  const accounts = useAccounts()
  const [minting, setMinting] = useState(false)
  const rows = accounts.data ?? []
  const total = rows.reduce((s, a) => s + (a.balance ?? 0), 0)
  const mine = rows.find((a) => a.user_id === user?.id)

  const columns = [
    {
      field: 'display_name',
      headerName: 'Account holder',
      flex: 1.2,
      minWidth: 180,
      renderCell: ({ row }) => (
        <Box>
          <strong>{row.display_name || row.username}</strong>
          <Box component="span" sx={{ color: 'text.secondary', ml: 1 }}>
            @{row.username}
          </Box>
        </Box>
      ),
    },
    { field: 'colony_name', headerName: 'Colony', flex: 1, minWidth: 140 },
    {
      field: 'balance',
      headerName: 'Balance',
      type: 'number',
      width: 160,
      renderCell: ({ value }) => (
        <Box sx={{ fontWeight: 600, fontVariantNumeric: 'tabular-nums', color: value > 0 ? 'text.primary' : 'text.secondary' }}>
          {formatAmount(value)}
        </Box>
      ),
    },
  ]

  return (
    <>
      <PageHeader
        title="Accounts"
        description="Colony-local balances. Funds enter through mints and move through settled trades."
        actions={
          isAdmin(user) && (
            <Button variant="contained" startIcon={<AddOutlined />} onClick={() => setMinting(true)}>
              Issue funds
            </Button>
          )
        }
      />
      <QueryError error={accounts.error} onRetry={accounts.refetch} />
      <Grid container spacing={2} sx={{ mb: 2 }}>
        <Grid size={{ xs: 12, sm: 4 }}>
          <StatCard label="Your balance" value={mine ? formatAmount(mine.balance) : '—'} icon={AccountBalanceWalletOutlined} loading={accounts.isLoading} />
        </Grid>
        <Grid size={{ xs: 12, sm: 4 }}>
          <StatCard label="Visible accounts" value={formatAmount(rows.length)} tone="secondary" loading={accounts.isLoading} />
        </Grid>
        <Grid size={{ xs: 12, sm: 4 }}>
          <StatCard label="Total visible balance" value={formatAmount(total)} tone="success" loading={accounts.isLoading} />
        </Grid>
      </Grid>
      <Card>
        <DataTable
          rows={rows}
          columns={columns}
          getRowId={(r) => `${r.colony_id}:${r.user_id}`}
          loading={accounts.isLoading}
          initialSort={[{ field: 'balance', sort: 'desc' }]}
          pageSize={25}
          empty={{
            icon: AccountBalanceWalletOutlined,
            title: 'No funded accounts',
            description: 'Issue funds to a colony member to activate trading.',
          }}
        />
      </Card>
      {minting && <MintDialog open onClose={() => setMinting(false)} />}
    </>
  )
}
