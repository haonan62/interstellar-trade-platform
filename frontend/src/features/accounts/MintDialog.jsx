import { useState } from 'react'
import { Alert, InputAdornment, MenuItem, TextField } from '@mui/material'
import { api } from '../../api/endpoints'
import { useAction, useDirectory } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { canManageColony } from '../../auth/permissions'
import FormDialog from '../../components/FormDialog'
import { useNotify } from '../../components/Notifications'
import { formatAmount } from '../../lib/format'

export default function MintDialog({ open, onClose, preset }) {
  const { user } = useSession()
  const notify = useNotify()
  const dir = useDirectory()
  const manageable = dir.colonies.filter((c) => canManageColony(user, c.id))
  const [form, setForm] = useState({
    colony_id: preset?.colony_id ?? (manageable.length === 1 ? manageable[0].id : ''),
    user_id: preset?.user_id ?? '',
    amount: '',
  })
  const colony = dir.colonyById[form.colony_id]
  const members = dir.users.filter((u) => u.colony_id === form.colony_id)
  const remaining = colony?.mint_cap ? colony.mint_cap - (colony.minted_total ?? 0) : null
  const amount = Number(form.amount)
  const valid = form.colony_id && form.user_id && Number.isInteger(amount) && amount > 0 && (remaining == null || amount <= remaining)

  const mint = useAction(() => api.mint({ colony_id: form.colony_id, user_id: form.user_id, amount }), {
    onSuccess: (res) => {
      notify.success(
        res.status === 202
          ? 'Dual control is on: the mint was proposed and awaits a second administrator.'
          : `Issued ${formatAmount(amount)} credits.`,
      )
      onClose()
    },
  })

  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title="Issue funds"
      description="Mint new credits into a colony member's account. Every mint is recorded in the colony ledger."
      submitLabel="Issue funds"
      onSubmit={() => mint.mutate()}
      pending={mint.isPending}
      error={mint.error}
      disabled={!valid}
    >
      <TextField
        select
        label="Colony"
        value={form.colony_id}
        onChange={(e) => setForm({ ...form, colony_id: e.target.value, user_id: '' })}
      >
        {manageable.map((c) => (
          <MenuItem key={c.id} value={c.id}>
            {c.name}
          </MenuItem>
        ))}
      </TextField>
      <TextField select label="Account holder" value={form.user_id} onChange={(e) => setForm({ ...form, user_id: e.target.value })} disabled={!form.colony_id}>
        {members.map((u) => (
          <MenuItem key={u.id} value={u.id}>
            {u.display_name || u.username}
          </MenuItem>
        ))}
      </TextField>
      <TextField
        label="Amount"
        type="number"
        value={form.amount}
        onChange={(e) => setForm({ ...form, amount: e.target.value })}
        helperText={remaining != null ? `Remaining mint allowance: ${formatAmount(remaining)}` : ' '}
        error={remaining != null && amount > remaining}
        slotProps={{ htmlInput: { min: 1, step: 1 }, input: { endAdornment: <InputAdornment position="end">credits</InputAdornment> } }}
      />
      {colony?.require_dual_control && <Alert severity="info">This colony requires dual control: a second administrator must approve.</Alert>}
    </FormDialog>
  )
}
