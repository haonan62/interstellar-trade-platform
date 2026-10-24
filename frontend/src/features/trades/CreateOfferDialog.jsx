import { useState } from 'react'
import { Alert, InputAdornment, MenuItem, TextField } from '@mui/material'
import { api } from '../../api/endpoints'
import { useAction, useDirectory } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { isSuperAdmin } from '../../auth/permissions'
import FormDialog from '../../components/FormDialog'
import { useNotify } from '../../components/Notifications'

const TTL_OPTIONS = [
  { label: 'Default (7 days)', value: 0 },
  { label: '1 hour', value: 3600 },
  { label: '1 day', value: 86400 },
  { label: '3 days', value: 3 * 86400 },
  { label: '30 days', value: 30 * 86400 },
]

export default function CreateOfferDialog({ open, onClose }) {
  const { user } = useSession()
  const notify = useNotify()
  const dir = useDirectory()
  const initial = { seller_user_id: isSuperAdmin(user) ? '' : (user?.id ?? ''), buyer_user_id: '', asset: '', price: '', ttl_seconds: 0 }
  const [form, setForm] = useState(initial)
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value })

  // Only a super admin may create an offer on someone else's behalf.
  const sellers = isSuperAdmin(user)
    ? dir.users.filter((u) => u.colony_id && u.active !== false)
    : dir.users.filter((u) => u.id === user?.id)
  const seller = dir.userById[form.seller_user_id]
  const buyers = dir.users.filter((u) => u.colony_id && u.id !== form.seller_user_id && u.active !== false)
  const label = (u) => `${u.display_name || u.username} · ${dir.colonyName(u.colony_id)}`

  const create = useAction(
    () => {
      const body = {
        seller_user_id: form.seller_user_id,
        buyer_user_id: form.buyer_user_id,
        asset: form.asset.trim(),
        price: Number(form.price),
      }
      if (form.ttl_seconds) body.ttl_seconds = Number(form.ttl_seconds)
      return api.createOffer(body)
    },
    {
      onSuccess: () => {
        notify.success('Offer created. Export a relay bundle to deliver it to the buyer colony.')
        setForm(initial)
        onClose()
      },
    },
  )

  const price = Number(form.price)
  const valid = form.seller_user_id && form.buyer_user_id && form.asset.trim() && Number.isInteger(price) && price > 0
  const sameColony = seller && dir.userById[form.buyer_user_id]?.colony_id === seller.colony_id

  return (
    <FormDialog
      open={open}
      onClose={() => {
        create.reset()
        onClose()
      }}
      title="New trade offer"
      description="The seller signs the offer; the buyer's funds move into escrow when they accept."
      submitLabel="Create offer"
      onSubmit={() => create.mutate()}
      pending={create.isPending}
      error={create.error}
      disabled={!valid}
      maxWidth="sm"
    >
      <TextField select label="Seller" value={form.seller_user_id} onChange={set('seller_user_id')} disabled={!isSuperAdmin(user)}>
        {sellers.map((u) => (
          <MenuItem key={u.id} value={u.id}>
            {label(u)}
          </MenuItem>
        ))}
      </TextField>
      <TextField select label="Buyer" value={form.buyer_user_id} onChange={set('buyer_user_id')}>
        {buyers.map((u) => (
          <MenuItem key={u.id} value={u.id}>
            {label(u)}
          </MenuItem>
        ))}
      </TextField>
      {sameColony && <Alert severity="info">Buyer and seller are in the same colony.</Alert>}
      <TextField label="Asset" value={form.asset} onChange={set('asset')} placeholder="e.g. habitat-design-v2" />
      <TextField
        label="Price"
        type="number"
        value={form.price}
        onChange={set('price')}
        slotProps={{
          htmlInput: { min: 1, step: 1 },
          input: { endAdornment: <InputAdornment position="end">credits</InputAdornment> },
        }}
      />
      <TextField select label="Offer expires after" value={form.ttl_seconds} onChange={set('ttl_seconds')}>
        {TTL_OPTIONS.map((o) => (
          <MenuItem key={o.value} value={o.value}>
            {o.label}
          </MenuItem>
        ))}
      </TextField>
    </FormDialog>
  )
}
