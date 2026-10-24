import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Button, Card, Divider, InputAdornment, Stack, Tab, Tabs, TextField, Tooltip } from '@mui/material'
import AddOutlined from '@mui/icons-material/AddOutlined'
import SearchOutlined from '@mui/icons-material/SearchOutlined'
import SwapHorizOutlined from '@mui/icons-material/SwapHorizOutlined'
import { GridActionsCellItem } from '@mui/x-data-grid'
import CheckCircleOutline from '@mui/icons-material/CheckCircleOutline'
import BlockOutlined from '@mui/icons-material/BlockOutlined'
import UndoOutlined from '@mui/icons-material/UndoOutlined'
import GavelOutlined from '@mui/icons-material/GavelOutlined'
import { api } from '../../api/endpoints'
import { useAction, useTrades } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { canTrade } from '../../auth/permissions'
import PageHeader from '../../components/PageHeader'
import DataTable from '../../components/DataTable'
import ConfirmDialog from '../../components/ConfirmDialog'
import { TradeStatusChip } from '../../components/Chips'
import { QueryError } from '../../components/QueryState'
import { useNotify } from '../../components/Notifications'
import { OPEN_TRADE_STATUSES } from '../../lib/constants'
import { formatAmount, formatRelative } from '../../lib/format'
import CreateOfferDialog from './CreateOfferDialog'
import TradeDetailDrawer from './TradeDetailDrawer'
import { tradeActions } from './tradeActions'

const FILTERS = {
  all: () => true,
  open: (t) => OPEN_TRADE_STATUSES.has(t.status),
  completed: (t) => t.status === 'completed',
  closed: (t) => ['refunded', 'expired', 'cancelled'].includes(t.status),
}

// Confirmation copy for each action. `run` calls the API.
const ACTIONS = {
  accept: {
    title: 'Accept offer',
    message: (t) => `Accept ${t.asset} for ${formatAmount(t.price)} credits? The amount moves into escrow until settlement.`,
    label: 'Accept',
    run: (t) => api.acceptTrade(t.id),
    done: 'Offer accepted. Export a relay bundle to notify the seller colony.',
  },
  withdraw: {
    title: 'Withdraw offer',
    message: (t) => `Withdraw the offer for ${t.asset}? No funds have moved yet.`,
    label: 'Withdraw',
    danger: true,
    run: (t) => api.cancelTrade(t.id),
    done: 'Offer withdrawn.',
  },
  requestRefund: {
    title: 'Request cancellation',
    message: () =>
      'Send a signed cancellation request to the seller colony. You are refunded only when the seller colony confirms.',
    label: 'Send request',
    danger: true,
    run: (t) => api.cancelTrade(t.id),
    done: 'Cancellation request queued for relay.',
  },
  forceRefund: {
    title: 'Propose force refund',
    message: () =>
      'Refund the buyer without the seller colony’s confirmation. A second administrator must approve. If the seller colony already credited the seller, this creates money — confirm out-of-band first.',
    label: 'Propose refund',
    danger: true,
    run: (t) => api.forceRefund(t.id),
    done: 'Force refund proposed; it now awaits a second approval.',
  },
}

export default function TradesPage() {
  const { user } = useSession()
  const notify = useNotify()
  const trades = useTrades()
  const [params, setParams] = useSearchParams()
  const [filter, setFilter] = useState('all')
  const [search, setSearch] = useState('')
  const [selectedId, setSelectedId] = useState(null)
  const [pendingAction, setPendingAction] = useState(null) // { kind, trade }

  const creating = params.get('new') === '1'
  const setCreating = (on) => setParams(on ? { new: '1' } : {}, { replace: true })

  const run = useAction(({ kind, trade }) => ACTIONS[kind].run(trade), {
    onSuccess: (_, { kind }) => {
      notify.success(ACTIONS[kind].done)
      setPendingAction(null)
    },
  })

  const all = trades.data ?? []
  const counts = Object.fromEntries(Object.entries(FILTERS).map(([k, f]) => [k, all.filter(f).length]))
  const q = search.trim().toLowerCase()
  const rows = all
    .filter(FILTERS[filter])
    .filter(
      (t) =>
        !q ||
        [t.asset, t.seller_name, t.buyer_name, t.seller_colony_name, t.buyer_colony_name, t.id].some((v) =>
          String(v ?? '').toLowerCase().includes(q),
        ),
    )
  const selected = all.find((t) => t.id === selectedId) ?? null

  const ask = (kind, trade) => {
    run.reset()
    setPendingAction({ kind, trade })
  }

  const actionButtons = (t) => {
    const a = tradeActions(user, t)
    return [
      a.accept && (
        <Button key="accept" variant="contained" size="small" onClick={() => ask('accept', t)}>
          Accept
        </Button>
      ),
      a.withdraw && (
        <Button key="withdraw" color="error" variant="outlined" size="small" onClick={() => ask('withdraw', t)}>
          Withdraw
        </Button>
      ),
      a.requestRefund && (
        <Button key="refund" variant="outlined" size="small" onClick={() => ask('requestRefund', t)}>
          Request cancellation
        </Button>
      ),
      a.forceRefund && (
        <Button key="force" color="warning" variant="outlined" size="small" onClick={() => ask('forceRefund', t)}>
          Force refund
        </Button>
      ),
    ].filter(Boolean)
  }

  const columns = [
    { field: 'asset', headerName: 'Asset', flex: 1, minWidth: 140, renderCell: ({ value }) => <strong>{value}</strong> },
    { field: 'seller', headerName: 'Seller', flex: 1.2, minWidth: 170, valueGetter: (_, r) => `${r.seller_name} · ${r.seller_colony_name}` },
    { field: 'buyer', headerName: 'Buyer', flex: 1.2, minWidth: 170, valueGetter: (_, r) => `${r.buyer_name} · ${r.buyer_colony_name}` },
    { field: 'price', headerName: 'Price', type: 'number', width: 110, valueFormatter: (v) => formatAmount(v) },
    { field: 'status', headerName: 'Status', width: 190, renderCell: ({ value }) => <TradeStatusChip status={value} /> },
    {
      field: 'created_at',
      headerName: 'Created',
      width: 130,
      renderCell: ({ value }) => <Tooltip title={value}><span>{formatRelative(value)}</span></Tooltip>,
    },
    {
      field: 'actions',
      type: 'actions',
      width: 80,
      getActions: ({ row }) => {
        const a = tradeActions(user, row)
        const items = []
        if (a.accept) items.push(<GridActionsCellItem key="a" icon={<CheckCircleOutline color="success" />} label="Accept" onClick={() => ask('accept', row)} />)
        if (a.withdraw) items.push(<GridActionsCellItem key="w" icon={<BlockOutlined />} label="Withdraw offer" onClick={() => ask('withdraw', row)} showInMenu />)
        if (a.requestRefund) items.push(<GridActionsCellItem key="r" icon={<UndoOutlined />} label="Request cancellation" onClick={() => ask('requestRefund', row)} showInMenu />)
        if (a.forceRefund) items.push(<GridActionsCellItem key="f" icon={<GavelOutlined />} label="Propose force refund" onClick={() => ask('forceRefund', row)} showInMenu />)
        return items
      },
    },
  ]

  const action = pendingAction && ACTIONS[pendingAction.kind]

  return (
    <>
      <PageHeader
        title="Trades"
        description="Offers move between colonies in relay bundles: offer → acceptance (escrow) → settlement."
        actions={
          canTrade(user) && (
            <Button variant="contained" startIcon={<AddOutlined />} onClick={() => setCreating(true)}>
              New offer
            </Button>
          )
        }
      />
      <QueryError error={trades.error} onRetry={trades.refetch} />
      <Card>
        <Stack
          direction={{ xs: 'column', md: 'row' }}
          sx={{ px: 2, pt: 1, justifyContent: 'space-between', alignItems: { md: 'center' }, gap: 1 }}
        >
          <Tabs value={filter} onChange={(_, v) => setFilter(v)} sx={{ minHeight: 44, '& .MuiTab-root': { minHeight: 44 } }}>
            <Tab value="all" label={`All (${counts.all ?? 0})`} />
            <Tab value="open" label={`Open (${counts.open ?? 0})`} />
            <Tab value="completed" label={`Completed (${counts.completed ?? 0})`} />
            <Tab value="closed" label={`Closed (${counts.closed ?? 0})`} />
          </Tabs>
          <TextField
            placeholder="Search asset, party or colony"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            sx={{ maxWidth: { md: 300 }, pb: { xs: 1, md: 0 } }}
            slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchOutlined fontSize="small" /></InputAdornment> } }}
          />
        </Stack>
        <Divider />
        <DataTable
          rows={rows}
          columns={columns}
          loading={trades.isLoading}
          initialSort={[{ field: 'created_at', sort: 'desc' }]}
          onRowClick={({ row }) => setSelectedId(row.id)}
          sx={{ '& .MuiDataGrid-row': { cursor: 'pointer' } }}
          empty={{
            icon: SwapHorizOutlined,
            title: q || filter !== 'all' ? 'No matching trades' : 'No trades yet',
            description: q || filter !== 'all' ? 'Try another filter or search.' : 'Create an offer to start trading between colonies.',
          }}
        />
      </Card>

      <TradeDetailDrawer trade={selected} onClose={() => setSelectedId(null)} actions={selected && actionButtons(selected)} />
      {creating && <CreateOfferDialog open onClose={() => setCreating(false)} />}
      {action && (
        <ConfirmDialog
          open
          onClose={() => setPendingAction(null)}
          title={action.title}
          message={action.message(pendingAction.trade)}
          confirmLabel={action.label}
          danger={action.danger}
          onConfirm={() => run.mutate(pendingAction)}
          pending={run.isPending}
          error={run.error}
        />
      )}
    </>
  )
}
