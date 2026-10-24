import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, Stack, TextField } from '@mui/material'
import { useSession } from '../../auth/SessionProvider'
import { LoadingButton } from '../../components/LoadingButton'
import AuthLayout from './AuthLayout'

export default function BootstrapPage() {
  const { initialize } = useSession()
  const navigate = useNavigate()
  const [form, setForm] = useState({ username: 'admin', display_name: 'Administrator', password: '', bootstrap_token: '' })
  const [error, setError] = useState(null)
  const [pending, setPending] = useState(false)
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value })

  const submit = async (e) => {
    e.preventDefault()
    setPending(true)
    setError(null)
    try {
      const { bootstrap_token, ...body } = form
      await initialize(body, bootstrap_token)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err)
    } finally {
      setPending(false)
    }
  }

  return (
    <AuthLayout title="Set up the platform" subtitle="Create the first super administrator. This can only be done once.">
      <Stack component="form" spacing={2} onSubmit={submit} noValidate>
        {error && <Alert severity="error">{error.message}</Alert>}
        <TextField label="Username" value={form.username} onChange={set('username')} autoComplete="username" required />
        <TextField label="Display name" value={form.display_name} onChange={set('display_name')} />
        <TextField
          label="Password"
          type="password"
          value={form.password}
          onChange={set('password')}
          autoComplete="new-password"
          helperText="At least 8 characters."
          required
        />
        <TextField
          label="Bootstrap token"
          type="password"
          value={form.bootstrap_token}
          onChange={set('bootstrap_token')}
          helperText="The server's BOOTSTRAP_TOKEN, or the token printed in its start-up log."
          required
        />
        <LoadingButton
          type="submit"
          variant="contained"
          size="large"
          loading={pending}
          disabled={!form.username || !form.password || !form.bootstrap_token}
        >
          Create administrator
        </LoadingButton>
      </Stack>
    </AuthLayout>
  )
}
