import { useState } from 'react'
import { Box, Button, Card, LinearProgress, Stack, Tooltip, Typography } from '@mui/material'
import { GridActionsCellItem } from '@mui/x-data-grid'
import AddOutlined from '@mui/icons-material/AddOutlined'
import PublicOutlined from '@mui/icons-material/PublicOutlined'
import HandshakeOutlined from '@mui/icons-material/HandshakeOutlined'
import TuneOutlined from '@mui/icons-material/TuneOutlined'
import { useColonies } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { canManageColony, isSuperAdmin } from '../../auth/permissions'
import PageHeader from '../../components/PageHeader'
import DataTable from '../../components/DataTable'
import Mono from '../../components/Mono'
import { SoftChip } from '../../components/Chips'
import { QueryError } from '../../components/QueryState'
import { formatAmount, formatDateTime } from '../../lib/format'
import { CreateColonyDialog, PolicyDialog, TrustPeerDialog } from './ColonyDialogs'

function MintUsage({ colony }) {
  const cap = colony.mint_cap || 0
  const used = colony.minted_total || 0
  const pct = cap ? Math.min(100, (used / cap) * 100) : 0
  return (
    <Tooltip title={`${formatAmount(used)} of ${formatAmount(cap)} minted`}>
      <Box sx={{ width: '100%' }}>
        <Typography variant="caption" color="text.secondary">
          {formatAmount(used)} / {formatAmount(cap)}
        </Typography>
        <LinearProgress variant="determinate" value={pct} color={pct > 90 ? 'warning' : 'primary'} sx={{ height: 4, borderRadius: 2 }} />
      </Box>
    </Tooltip>
  )
}

export default function ColoniesPage() {
  const { user } = useSession()
  const colonies = useColonies()
  const [creating, setCreating] = useState(false)
  const [trusting, setTrusting] = useState(null)
  const [editingPolicy, setEditingPolicy] = useState(null)
  const byId = Object.fromEntries((colonies.data ?? []).map((c) => [c.id, c]))

  const columns = [
    {
      field: 'name',
      headerName: 'Colony',
      flex: 1,
      minWidth: 180,
      renderCell: ({ row }) => (
        <Stack sx={{ py: 1 }}>
          <strong>{row.name}</strong>
          <Mono value={row.id} copy />
        </Stack>
      ),
    },
    {
      field: 'trusted_colonies',
      headerName: 'Trusted peers',
      flex: 1.3,
      minWidth: 200,
      sortable: false,
      renderCell: ({ value }) => {
        const peers = Object.keys(value ?? {})
        return peers.length ? (
          <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', rowGap: 0.5, py: 1 }}>
            {peers.map((p) => (
              <SoftChip key={p} label={byId[p]?.name ?? p.slice(0, 10)} tone="primary" />
            ))}
          </Stack>
        ) : (
          <Typography variant="body2" color="text.secondary">
            None
          </Typography>
        )
      },
    },
    { field: 'minted_total', headerName: 'Mint usage', width: 180, renderCell: ({ row }) => <MintUsage colony={row} /> },
    {
      field: 'require_dual_control',
      headerName: 'Dual control',
      width: 120,
      renderCell: ({ value }) => <SoftChip label={value ? 'Required' : 'Off'} tone={value ? 'success' : 'default'} />,
    },
    { field: 'created_at', headerName: 'Created', width: 170, valueFormatter: (v) => formatDateTime(v) },
    {
      field: 'actions',
      type: 'actions',
      width: 60,
      getActions: ({ row }) =>
        [
          canManageColony(user, row.id) && (
            <GridActionsCellItem key="t" icon={<HandshakeOutlined />} label="Trust a peer colony" onClick={() => setTrusting(row)} showInMenu />
          ),
          isSuperAdmin(user) && (
            <GridActionsCellItem key="p" icon={<TuneOutlined />} label="Supply policy" onClick={() => setEditingPolicy(row)} showInMenu />
          ),
        ].filter(Boolean),
    },
  ]

  return (
    <>
      <PageHeader
        title="Colonies"
        description="Each colony keeps its own keys, ledger and accounts. Trade requires mutual trust."
        actions={
          isSuperAdmin(user) && (
            <Button variant="contained" startIcon={<AddOutlined />} onClick={() => setCreating(true)}>
              New colony
            </Button>
          )
        }
      />
      <QueryError error={colonies.error} onRetry={colonies.refetch} />
      <Card>
        <DataTable
          rows={colonies.data}
          columns={columns}
          loading={colonies.isLoading}
          getRowHeight={() => 'auto'}
          initialSort={[{ field: 'name', sort: 'asc' }]}
          empty={{
            icon: PublicOutlined,
            title: 'No colonies yet',
            description: isSuperAdmin(user) ? 'Create the first colony to start onboarding users.' : 'A super administrator has not created any colonies yet.',
          }}
        />
      </Card>
      {creating && <CreateColonyDialog open onClose={() => setCreating(false)} />}
      {trusting && <TrustPeerDialog colony={trusting} onClose={() => setTrusting(null)} />}
      {editingPolicy && <PolicyDialog colony={editingPolicy} onClose={() => setEditingPolicy(null)} />}
    </>
  )
}
