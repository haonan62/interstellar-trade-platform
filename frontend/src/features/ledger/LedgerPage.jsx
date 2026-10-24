import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Alert, Box, Card, Dialog, DialogContent, DialogTitle, Divider, MenuItem, Stack, TextField, Tooltip, Typography } from '@mui/material'
import ReceiptLongOutlined from '@mui/icons-material/ReceiptLongOutlined'
import VerifiedOutlined from '@mui/icons-material/VerifiedOutlined'
import ReportProblemOutlined from '@mui/icons-material/ReportProblemOutlined'
import { useColonies, useDirectory, useLedger } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import PageHeader from '../../components/PageHeader'
import DataTable from '../../components/DataTable'
import Mono from '../../components/Mono'
import { SoftChip } from '../../components/Chips'
import { QueryError } from '../../components/QueryState'
import { fontMono } from '../../app/theme'
import { formatDateTime, humanize } from '../../lib/format'

const TYPE_TONE = { mint: 'success', trade_settled: 'success', trade_completed: 'success', role_change: 'warning' }
const toneFor = (type) => TYPE_TONE[type] ?? (type?.includes('refund') || type?.includes('cancel') ? 'warning' : 'primary')

// chainBreaks returns the seqs whose prev_hash does not match the previous
// entry's hash within the loaded page. The server verifies the full hashes;
// this only flags gaps visible in what was fetched.
function chainBreaks(entries) {
  const out = new Set()
  for (let i = 1; i < entries.length; i++) {
    if (entries[i].prev_hash !== entries[i - 1].hash || entries[i].seq !== entries[i - 1].seq + 1) out.add(entries[i].seq)
  }
  return out
}

export default function LedgerPage() {
  const { user } = useSession()
  const colonies = useColonies()
  const dir = useDirectory()
  const [params, setParams] = useSearchParams()
  const colonyId = params.get('colony') || user?.colony_id || colonies.data?.[0]?.id || ''
  const ledger = useLedger(colonyId)
  const [open, setOpen] = useState(null)
  const entries = ledger.data ?? []
  const breaks = chainBreaks(entries)

  const columns = [
    { field: 'seq', headerName: 'Seq', type: 'number', width: 80 },
    { field: 'time', headerName: 'Time', width: 180, valueFormatter: (v) => formatDateTime(v) },
    { field: 'type', headerName: 'Event', width: 220, renderCell: ({ value }) => <SoftChip label={humanize(value)} tone={toneFor(value)} /> },
    { field: 'actor_user_id', headerName: 'Actor', flex: 0.8, minWidth: 140, valueFormatter: (v) => (v ? dir.userName(v) : 'System') },
    {
      field: 'payload',
      headerName: 'Details',
      flex: 1.5,
      minWidth: 220,
      sortable: false,
      valueGetter: (v) => JSON.stringify(v),
      renderCell: ({ value }) => (
        <Box component="span" sx={{ fontFamily: fontMono, fontSize: 11, color: 'text.secondary', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {value}
        </Box>
      ),
    },
    {
      field: 'hash',
      headerName: 'Hash',
      width: 170,
      renderCell: ({ row }) => (
        <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
          {breaks.has(row.seq) && (
            <Tooltip title="prev_hash does not match the previous entry">
              <ReportProblemOutlined color="error" sx={{ fontSize: 16 }} />
            </Tooltip>
          )}
          <Mono value={row.hash} />
        </Stack>
      ),
    },
  ]

  return (
    <>
      <PageHeader
        title="Ledger"
        description="Append-only, hash-chained record of every balance change and trade event in a colony."
        actions={
          <TextField
            select
            label="Colony"
            value={colonyId}
            onChange={(e) => setParams({ colony: e.target.value })}
            sx={{ minWidth: 220 }}
          >
            {(colonies.data ?? []).map((c) => (
              <MenuItem key={c.id} value={c.id}>
                {c.name}
              </MenuItem>
            ))}
          </TextField>
        }
      />
      <QueryError error={ledger.error} onRetry={ledger.refetch} />
      {entries.length > 1 && (
        <Alert
          severity={breaks.size ? 'error' : 'success'}
          icon={breaks.size ? <ReportProblemOutlined /> : <VerifiedOutlined />}
          sx={{ mb: 2 }}
        >
          {breaks.size
            ? `${breaks.size} entr${breaks.size === 1 ? 'y breaks' : 'ies break'} the hash chain in the loaded range.`
            : `Hash links are continuous across the ${entries.length} loaded entries (seq ${entries[0].seq}–${entries[entries.length - 1].seq}).`}
        </Alert>
      )}
      <Card>
        <DataTable
          rows={entries}
          columns={columns}
          getRowId={(r) => r.seq}
          loading={ledger.isLoading}
          pageSize={25}
          initialSort={[{ field: 'seq', sort: 'desc' }]}
          onRowClick={({ row }) => setOpen(row)}
          sx={{ '& .MuiDataGrid-row': { cursor: 'pointer' } }}
          empty={{
            icon: ReceiptLongOutlined,
            title: colonyId ? 'No ledger entries' : 'Select a colony',
            description: colonyId ? 'Entries appear once funds are minted or trades progress.' : undefined,
          }}
        />
      </Card>

      <Dialog open={!!open} onClose={() => setOpen(null)} fullWidth maxWidth="md">
        {open && (
          <>
            <DialogTitle>
              Entry #{open.seq} · {humanize(open.type)}
            </DialogTitle>
            <DialogContent>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                {formatDateTime(open.time)} by {open.actor_user_id ? dir.userName(open.actor_user_id) : 'system'}
              </Typography>
              <Box component="pre" sx={{ m: 0, p: 2, borderRadius: 1, bgcolor: 'action.hover', overflowX: 'auto', fontSize: 12, fontFamily: fontMono }}>
                {JSON.stringify(open.payload, null, 2)}
              </Box>
              <Divider sx={{ my: 2 }} />
              <Stack spacing={1}>
                <Typography variant="caption" color="text.secondary">
                  Hash
                </Typography>
                <Mono value={open.hash} short={false} copy />
                <Typography variant="caption" color="text.secondary">
                  Previous hash
                </Typography>
                <Mono value={open.prev_hash} short={false} copy />
              </Stack>
            </DialogContent>
          </>
        )}
      </Dialog>
    </>
  )
}
