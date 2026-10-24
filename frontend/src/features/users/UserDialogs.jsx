import { useState } from 'react'
import { Alert, Box, Checkbox, FormControlLabel, FormGroup, FormLabel, MenuItem, TextField, Typography } from '@mui/material'
import { api } from '../../api/endpoints'
import { useAction, useColonies } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { canManageColony, isSuperAdmin } from '../../auth/permissions'
import FormDialog from '../../components/FormDialog'
import Mono from '../../components/Mono'
import { useNotify } from '../../components/Notifications'
import { ROLES, ROLE_META } from '../../lib/constants'
import { formatDateTime } from '../../lib/format'

function RolePicker({ value, onChange, allowSuperAdmin }) {
  const toggle = (r) => onChange(value.includes(r) ? value.filter((x) => x !== r) : [...value, r])
  return (
    <Box>
      <FormLabel sx={{ fontSize: '0.8125rem' }}>Roles</FormLabel>
      <FormGroup>
        {ROLES.filter((r) => allowSuperAdmin || r !== 'super_admin').map((r) => (
          <FormControlLabel
            key={r}
            control={<Checkbox size="small" checked={value.includes(r)} onChange={() => toggle(r)} />}
            label={
              <Box>
                <Typography variant="body2" sx={{ fontWeight: 500 }}>
                  {ROLE_META[r].label}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {ROLE_META[r].description}
                </Typography>
              </Box>
            }
            sx={{ alignItems: 'flex-start', mt: 0.5, '& .MuiCheckbox-root': { pt: 0.25 } }}
          />
        ))}
      </FormGroup>
    </Box>
  )
}

export function CreateUserDialog({ open, onClose }) {
  const { user } = useSession()
  const notify = useNotify()
  const colonies = useColonies()
  const manageable = (colonies.data ?? []).filter((c) => canManageColony(user, c.id))
  const [form, setForm] = useState({
    username: '',
    display_name: '',
    password: '',
    colony_id: manageable.length === 1 ? manageable[0].id : '',
    roles: ['trader'],
  })
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value })
  const create = useAction(() => api.createUser({ ...form, username: form.username.trim() }), {
    onSuccess: () => {
      notify.success(`User @${form.username.trim()} created.`)
      onClose()
    },
  })
  const valid = form.username.trim() && form.password.length >= 8 && form.roles.length && (form.colony_id || isSuperAdmin(user))

  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title="Create user"
      submitLabel="Create user"
      onSubmit={() => create.mutate()}
      pending={create.isPending}
      error={create.error}
      disabled={!valid}
      maxWidth="sm"
    >
      <TextField label="Username" value={form.username} onChange={set('username')} autoComplete="off" autoFocus />
      <TextField label="Display name" value={form.display_name} onChange={set('display_name')} />
      <TextField
        label="Initial password"
        type="password"
        value={form.password}
        onChange={set('password')}
        autoComplete="new-password"
        helperText="At least 8 characters. Ask the user to change it after the first sign-in."
      />
      <TextField select label="Colony" value={form.colony_id} onChange={set('colony_id')}>
        {isSuperAdmin(user) && (
          <MenuItem value="">
            <em>No colony (platform operator)</em>
          </MenuItem>
        )}
        {manageable.map((c) => (
          <MenuItem key={c.id} value={c.id}>
            {c.name}
          </MenuItem>
        ))}
      </TextField>
      <RolePicker value={form.roles} onChange={(roles) => setForm({ ...form, roles })} allowSuperAdmin={isSuperAdmin(user)} />
    </FormDialog>
  )
}

export function EditRolesDialog({ target, onClose }) {
  const { user } = useSession()
  const notify = useNotify()
  const [roles, setRoles] = useState(target.roles ?? [])
  const save = useAction(() => api.setUserRoles(target.id, roles), {
    onSuccess: () => {
      notify.success('Roles updated.')
      onClose()
    },
  })
  return (
    <FormDialog
      open
      onClose={onClose}
      title={`Roles · ${target.display_name || target.username}`}
      description="Role changes take effect immediately and are recorded in the colony ledger."
      submitLabel="Save roles"
      onSubmit={() => save.mutate()}
      pending={save.isPending}
      error={save.error}
      disabled={!roles.length}
    >
      <RolePicker value={roles} onChange={setRoles} allowSuperAdmin={isSuperAdmin(user)} />
    </FormDialog>
  )
}

export function ResetTokenDialog({ target, onClose }) {
  const issue = useAction(() => api.issueReset(target.id))
  const token = issue.data
  return (
    <FormDialog
      open
      onClose={onClose}
      title={`Reset password · ${target.display_name || target.username}`}
      description={
        token
          ? 'Give this one-time token to the user over a trusted channel. It is shown only once.'
          : 'Issue a one-time token the user can redeem on the reset-password page. Their sessions are signed out when they use it.'
      }
      submitLabel={token ? undefined : 'Issue token'}
      onSubmit={token ? undefined : () => issue.mutate()}
      pending={issue.isPending}
      error={issue.error}
    >
      {token && (
        <>
          <Alert severity="warning">Copy it now. Only a hash is stored on the server.</Alert>
          <Box sx={{ p: 1.5, borderRadius: 1, bgcolor: 'action.hover', wordBreak: 'break-all' }}>
            <Mono value={token.reset_token} short={false} copy color="text.primary" />
          </Box>
          <Typography variant="caption" color="text.secondary">
            Expires {formatDateTime(token.expires_at)}
          </Typography>
        </>
      )}
    </FormDialog>
  )
}
