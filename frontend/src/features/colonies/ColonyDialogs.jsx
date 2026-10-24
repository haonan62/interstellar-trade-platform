import { useState } from 'react'
import { Alert, FormControlLabel, MenuItem, Switch, TextField } from '@mui/material'
import { api } from '../../api/endpoints'
import { useAction, useColonies } from '../../api/queries'
import FormDialog from '../../components/FormDialog'
import { useNotify } from '../../components/Notifications'
import { formatAmount } from '../../lib/format'

export function CreateColonyDialog({ open, onClose }) {
  const notify = useNotify()
  const [name, setName] = useState('')
  const create = useAction(() => api.createColony({ name: name.trim() }), {
    onSuccess: (c) => {
      notify.success(`Colony ${c?.name ?? name} created.`)
      onClose()
    },
  })
  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title="Create colony"
      description="A colony gets its own signing key, ledger and accounts."
      submitLabel="Create colony"
      onSubmit={() => create.mutate()}
      pending={create.isPending}
      error={create.error}
      disabled={!name.trim()}
    >
      <TextField label="Colony name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Proxima Base" autoFocus />
    </FormDialog>
  )
}

export function TrustPeerDialog({ colony, onClose }) {
  const notify = useNotify()
  const colonies = useColonies()
  const [peer, setPeer] = useState('')
  const candidates = (colonies.data ?? []).filter((c) => c.id !== colony.id && !colony.trusted_colonies?.[c.id])
  const trust = useAction(() => api.trustPeer(colony.id, peer), {
    onSuccess: () => {
      notify.success('Trust link saved.')
      onClose()
    },
  })
  return (
    <FormDialog
      open
      onClose={onClose}
      title={`Trust a peer of ${colony.name}`}
      description="The colony will accept envelopes signed by the peer's colony key. Trust is one-directional: configure it on both sides."
      submitLabel="Trust colony"
      onSubmit={() => trust.mutate()}
      pending={trust.isPending}
      error={trust.error}
      disabled={!peer}
    >
      {candidates.length ? (
        <TextField select label="Peer colony" value={peer} onChange={(e) => setPeer(e.target.value)}>
          {candidates.map((c) => (
            <MenuItem key={c.id} value={c.id}>
              {c.name}
            </MenuItem>
          ))}
        </TextField>
      ) : (
        <Alert severity="info">This colony already trusts every other colony.</Alert>
      )}
    </FormDialog>
  )
}

export function PolicyDialog({ colony, onClose }) {
  const notify = useNotify()
  const [mintCap, setMintCap] = useState(String(colony.mint_cap ?? ''))
  const [dual, setDual] = useState(!!colony.require_dual_control)
  const cap = Number(mintCap)
  const capChanged = mintCap !== '' && cap !== colony.mint_cap
  const dualChanged = dual !== !!colony.require_dual_control
  const valid = (capChanged || dualChanged) && (!capChanged || (Number.isInteger(cap) && cap >= (colony.minted_total ?? 0) && cap > 0))

  const save = useAction(
    () => {
      const body = {}
      if (capChanged) body.mint_cap = cap
      if (dualChanged) body.require_dual_control = dual
      return api.setPolicy(colony.id, body)
    },
    {
      onSuccess: (res) => {
        notify.success(res.status === 202 ? 'Policy change proposed; a second administrator must approve.' : 'Policy updated.')
        onClose()
      },
    },
  )

  return (
    <FormDialog
      open
      onClose={onClose}
      title={`Supply policy · ${colony.name}`}
      description="Limits how much money can ever be minted in this colony and whether changes need a second approver."
      submitLabel="Save policy"
      onSubmit={() => save.mutate()}
      pending={save.isPending}
      error={save.error}
      disabled={!valid}
    >
      <TextField
        label="Mint cap"
        type="number"
        value={mintCap}
        onChange={(e) => setMintCap(e.target.value)}
        helperText={`Already minted: ${formatAmount(colony.minted_total ?? 0)}. The cap cannot go below that.`}
        slotProps={{ htmlInput: { min: colony.minted_total ?? 1, step: 1 } }}
      />
      <FormControlLabel
        control={<Switch checked={dual} onChange={(e) => setDual(e.target.checked)} />}
        label="Require dual control for mints, refunds and policy changes"
      />
      {colony.require_dual_control && <Alert severity="info">Dual control is on: this change will be proposed, not applied.</Alert>}
    </FormDialog>
  )
}
