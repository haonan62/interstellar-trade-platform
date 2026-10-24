import { useState } from 'react'
import { Box, Button, Card, Divider, Stack, Tab, Tabs, Tooltip, Typography } from '@mui/material'
import FactCheckOutlined from '@mui/icons-material/FactCheckOutlined'
import { api } from '../../api/endpoints'
import { useAction, useApprovals, useDirectory } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import PageHeader from '../../components/PageHeader'
import DataTable from '../../components/DataTable'
import ConfirmDialog from '../../components/ConfirmDialog'
import { ApprovalStatusChip } from '../../components/Chips'
import { QueryError } from '../../components/QueryState'
import { useNotify } from '../../components/Notifications'
import { APPROVAL_TYPE } from '../../lib/constants'
import { formatAmount, formatDateTime, formatRelative } from '../../lib/format'

function describe(a, dir) {
  switch (a.type) {
    case 'mint':
      return `Mint ${formatAmount(a.amount)} credits to ${dir.userName(a.user_id)}`
    case 'force_refund':
      return `Refund ${formatAmount(a.amount)} credits on trade ${a.trade_id?.slice(0, 14)}…`
    case 'policy': {
      const parts = []
      if (a.mint_cap != null) parts.push(`mint cap → ${formatAmount(a.mint_cap)}`)
      if (a.require_dual_control != null) parts.push(`dual control → ${a.require_dual_control ? 'on' : 'off'}`)
      return parts.join(', ') || 'Policy change'
    }
    default:
      return a.type
  }
}

export default function ApprovalsPage() {
  const { user } = useSession()
  const notify = useNotify()
  const approvals = useApprovals()
  const dir = useDirectory()
  const [tab, setTab] = useState('pending')
  const [decision, setDecision] = useState(null) // { approve, action }

  const decide = useAction(({ approve, action }) => (approve ? api.approve(action.id) : api.reject(action.id)), {
    onSuccess: (_, { approve }) => {
      notify.success(approve ? 'Approved and applied.' : 'Rejected.')
      setDecision(null)
    },
  })

  const all = approvals.data ?? []
  const rows = all.filter((a) => (tab === 'pending' ? a.status === 'pending' : a.status !== 'pending'))

  const columns = [
    { field: 'type', headerName: 'Type', width: 140, valueFormatter: (v) => APPROVAL_TYPE[v] ?? v },
    { field: 'summary', headerName: 'Request', flex: 1.6, minWidth: 240, valueGetter: (_, r) => describe(r, dir) },
    { field: 'colony_id', headerName: 'Colony', width: 140, valueFormatter: (v) => dir.colonyName(v) },
    {
      field: 'proposed_by',
      headerName: 'Proposed by',
      width: 170,
      renderCell: ({ row }) => (
        <Box sx={{ lineHeight: 1.3 }}>
          <Typography variant="body2">{dir.userName(row.proposed_by)}</Typography>
          <Typography variant="caption" color="text.secondary">
            {formatRelative(row.proposed_at)}
          </Typography>
        </Box>
      ),
    },
    tab === 'pending'
      ? { field: 'expires_at', headerName: 'Expires', width: 170, valueFormatter: (v) => formatDateTime(v) }
      : { field: 'decided_at', headerName: 'Decided', width: 170, valueGetter: (_, r) => r.decided_at, valueFormatter: (v) => formatDateTime(v) },
    { field: 'status', headerName: 'Status', width: 120, renderCell: ({ value }) => <ApprovalStatusChip status={value} /> },
    ...(tab === 'pending'
      ? [
          {
            field: 'decide',
            headerName: '',
            width: 190,
            sortable: false,
            renderCell: ({ row }) => {
              const own = row.proposed_by === user?.id
              return (
                <Tooltip title={own ? 'A different administrator must decide your own proposal.' : ''}>
                  <Stack direction="row" spacing={1}>
                    <Button size="small" variant="contained" disabled={own} onClick={() => (decide.reset(), setDecision({ approve: true, action: row }))}>
                      Approve
                    </Button>
                    <Button size="small" color="error" disabled={own} onClick={() => (decide.reset(), setDecision({ approve: false, action: row }))}>
                      Reject
                    </Button>
                  </Stack>
                </Tooltip>
              )
            },
          },
        ]
      : []),
  ]

  const pendingCount = all.filter((a) => a.status === 'pending').length
  return (
    <>
      <PageHeader
        title="Approvals"
        description="Dual control: mints, force refunds and policy changes in protected colonies need a second administrator."
      />
      <QueryError error={approvals.error} onRetry={approvals.refetch} />
      <Card>
        <Tabs value={tab} onChange={(_, v) => setTab(v)} sx={{ px: 2, minHeight: 44, '& .MuiTab-root': { minHeight: 44 } }}>
          <Tab value="pending" label={`Pending (${pendingCount})`} />
          <Tab value="history" label="History" />
        </Tabs>
        <Divider />
        <DataTable
          rows={rows}
          columns={columns}
          loading={approvals.isLoading}
          getRowHeight={() => 'auto'}
          initialSort={[{ field: tab === 'pending' ? 'expires_at' : 'decided_at', sort: tab === 'pending' ? 'asc' : 'desc' }]}
          sx={{ '& .MuiDataGrid-cell': { py: 1 } }}
          empty={{
            icon: FactCheckOutlined,
            title: tab === 'pending' ? 'No pending approvals' : 'No decisions yet',
            description: tab === 'pending' ? 'Proposals from colonies with dual control appear here.' : undefined,
          }}
        />
      </Card>
      {decision && (
        <ConfirmDialog
          open
          onClose={() => setDecision(null)}
          title={decision.approve ? 'Approve request' : 'Reject request'}
          message={`${describe(decision.action, dir)}.${
            decision.approve && decision.action.type === 'force_refund'
              ? ' Confirm out-of-band that the seller colony did not credit the seller; otherwise this creates money.'
              : ''
          }`}
          confirmLabel={decision.approve ? 'Approve' : 'Reject'}
          danger={!decision.approve || decision.action.type === 'force_refund'}
          onConfirm={() => decide.mutate(decision)}
          pending={decide.isPending}
          error={decide.error}
        />
      )}
    </>
  )
}
