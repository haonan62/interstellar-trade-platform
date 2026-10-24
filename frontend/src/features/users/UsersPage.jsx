import { useState } from 'react'
import { Avatar, Box, Button, Card, Divider, InputAdornment, Stack, TextField, Typography } from '@mui/material'
import { GridActionsCellItem } from '@mui/x-data-grid'
import AddOutlined from '@mui/icons-material/AddOutlined'
import PeopleOutlined from '@mui/icons-material/PeopleOutlined'
import SearchOutlined from '@mui/icons-material/SearchOutlined'
import BadgeOutlined from '@mui/icons-material/BadgeOutlined'
import KeyOutlined from '@mui/icons-material/KeyOutlined'
import PhonelinkLockOutlined from '@mui/icons-material/PhonelinkLockOutlined'
import PersonOffOutlined from '@mui/icons-material/PersonOffOutlined'
import PersonOutlined from '@mui/icons-material/PersonOutlined'
import { api } from '../../api/endpoints'
import { useAction, useDirectory } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { canManageColony, isAdmin } from '../../auth/permissions'
import PageHeader from '../../components/PageHeader'
import DataTable from '../../components/DataTable'
import ConfirmDialog from '../../components/ConfirmDialog'
import { RoleChips, SoftChip } from '../../components/Chips'
import { QueryError } from '../../components/QueryState'
import { useNotify } from '../../components/Notifications'
import { formatDateTime, initials } from '../../lib/format'
import { CreateUserDialog, EditRolesDialog, ResetTokenDialog } from './UserDialogs'

export default function UsersPage() {
  const { user } = useSession()
  const notify = useNotify()
  const dir = useDirectory()
  const [search, setSearch] = useState('')
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState(null)
  const [resetting, setResetting] = useState(null)
  const [confirm, setConfirm] = useState(null) // { kind: 'active' | 'mfa', target }

  const run = useAction(
    ({ kind, target }) => (kind === 'mfa' ? api.resetMFA(target.id) : api.setUserActive(target.id, !target.active)),
    {
      onSuccess: (_, { kind, target }) => {
        notify.success(kind === 'mfa' ? 'MFA reset; the user must enrol again.' : target.active ? 'User deactivated.' : 'User reactivated.')
        setConfirm(null)
      },
    },
  )

  // super_admins can manage platform operators (no colony) as well.
  const manageable = (u) => u.id !== user?.id && (canManageColony(user, u.colony_id) || (!u.colony_id && user?.roles?.includes('super_admin')))
  const q = search.trim().toLowerCase()
  const rows = dir.users.filter(
    (u) => !q || [u.username, u.display_name, dir.colonyName(u.colony_id)].some((v) => String(v ?? '').toLowerCase().includes(q)),
  )

  const columns = [
    {
      field: 'display_name',
      headerName: 'User',
      flex: 1.3,
      minWidth: 200,
      valueGetter: (_, r) => r.display_name || r.username,
      renderCell: ({ row }) => (
        <Stack direction="row" spacing={1.25} sx={{ alignItems: 'center', minWidth: 0 }}>
          <Avatar sx={{ width: 28, height: 28, fontSize: '0.7rem', bgcolor: row.active === false ? 'action.disabled' : 'primary.main' }}>
            {initials(row.display_name || row.username)}
          </Avatar>
          <Box sx={{ minWidth: 0, lineHeight: 1.25 }}>
            <Typography variant="body2" sx={{ fontWeight: 600 }} noWrap>
              {row.display_name || row.username}
              {row.id === user?.id && (
                <Box component="span" sx={{ color: 'text.secondary', fontWeight: 400 }}>
                  {' '}
                  (you)
                </Box>
              )}
            </Typography>
            <Typography variant="caption" color="text.secondary" noWrap>
              @{row.username}
            </Typography>
          </Box>
        </Stack>
      ),
    },
    { field: 'colony', headerName: 'Colony', flex: 0.8, minWidth: 130, valueGetter: (_, r) => (r.colony_id ? dir.colonyName(r.colony_id) : 'Platform') },
    { field: 'roles', headerName: 'Roles', flex: 1.4, minWidth: 220, sortable: false, renderCell: ({ value }) => <RoleChips roles={value} /> },
    {
      field: 'active',
      headerName: 'Status',
      width: 150,
      renderCell: ({ row }) => (
        <Stack direction="row" spacing={0.5}>
          <SoftChip label={row.active === false ? 'Deactivated' : 'Active'} tone={row.active === false ? 'default' : 'success'} />
          {row.mfa_enabled && <SoftChip label="MFA" tone="info" />}
        </Stack>
      ),
    },
    { field: 'created_at', headerName: 'Created', width: 170, valueFormatter: (v) => formatDateTime(v) },
    {
      field: 'actions',
      type: 'actions',
      width: 60,
      getActions: ({ row }) =>
        manageable(row)
          ? [
              <GridActionsCellItem key="r" icon={<BadgeOutlined />} label="Edit roles" onClick={() => setEditing(row)} showInMenu />,
              <GridActionsCellItem key="p" icon={<KeyOutlined />} label="Issue password reset" onClick={() => setResetting(row)} showInMenu />,
              row.mfa_enabled && (
                <GridActionsCellItem
                  key="m"
                  icon={<PhonelinkLockOutlined />}
                  label="Reset MFA"
                  onClick={() => {
                    run.reset()
                    setConfirm({ kind: 'mfa', target: row })
                  }}
                  showInMenu
                />
              ),
              <GridActionsCellItem
                key="a"
                icon={row.active === false ? <PersonOutlined /> : <PersonOffOutlined />}
                label={row.active === false ? 'Reactivate' : 'Deactivate'}
                onClick={() => {
                  run.reset()
                  setConfirm({ kind: 'active', target: row })
                }}
                showInMenu
              />,
            ].filter(Boolean)
          : [],
    },
  ]

  const t = confirm?.target
  return (
    <>
      <PageHeader
        title="Users"
        description="Operators, colony administrators and traders. Access is role-based and scoped to a colony."
        actions={
          isAdmin(user) && (
            <Button variant="contained" startIcon={<AddOutlined />} onClick={() => setCreating(true)}>
              New user
            </Button>
          )
        }
      />
      <QueryError error={dir.error} />
      <Card>
        <Box sx={{ p: 2 }}>
          <TextField
            placeholder="Search name, username or colony"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            sx={{ maxWidth: 320 }}
            slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchOutlined fontSize="small" /></InputAdornment> } }}
          />
        </Box>
        <Divider />
        <DataTable
          rows={rows}
          columns={columns}
          loading={dir.isLoading}
          pageSize={25}
          initialSort={[{ field: 'display_name', sort: 'asc' }]}
          empty={{ icon: PeopleOutlined, title: q ? 'No matching users' : 'No users yet', description: q ? 'Try another search.' : 'Create a user to grant access.' }}
        />
      </Card>

      {creating && <CreateUserDialog open onClose={() => setCreating(false)} />}
      {editing && <EditRolesDialog target={editing} onClose={() => setEditing(null)} />}
      {resetting && <ResetTokenDialog target={resetting} onClose={() => setResetting(null)} />}
      {confirm && (
        <ConfirmDialog
          open
          onClose={() => setConfirm(null)}
          title={confirm.kind === 'mfa' ? 'Reset MFA' : t.active === false ? 'Reactivate user' : 'Deactivate user'}
          message={
            confirm.kind === 'mfa'
              ? `Remove two-factor authentication from @${t.username}? They will need to enrol again.`
              : t.active === false
                ? `Allow @${t.username} to sign in again?`
                : `@${t.username} will be signed out everywhere and unable to sign in.`
          }
          confirmLabel={confirm.kind === 'mfa' ? 'Reset MFA' : t.active === false ? 'Reactivate' : 'Deactivate'}
          danger={confirm.kind === 'mfa' || t.active !== false}
          onConfirm={() => run.mutate(confirm)}
          pending={run.isPending}
          error={run.error}
        />
      )}
    </>
  )
}
