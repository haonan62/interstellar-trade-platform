import { useState } from 'react'
import { Link as RouterLink, useSearchParams } from 'react-router-dom'
import { Alert, Button, Link, Stack, TextField, Typography } from '@mui/material'
import { api } from '../../api/endpoints'
import { LoadingButton } from '../../components/LoadingButton'
import AuthLayout from './AuthLayout'

export default function ResetPasswordPage() {
  const [params] = useSearchParams()
  const [form, setForm] = useState({ reset_token: params.get('token') ?? '', new_password: '', confirm: '' })
  const [error, setError] = useState(null)
  const [pending, setPending] = useState(false)
  const [done, setDone] = useState(false)
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value })
  const mismatch = form.confirm && form.confirm !== form.new_password

  const submit = async (e) => {
    e.preventDefault()
    setPending(true)
    setError(null)
    try {
      await api.completeReset({ reset_token: form.reset_token.trim(), new_password: form.new_password })
      setDone(true)
    } catch (err) {
      setError(err)
    } finally {
      setPending(false)
    }
  }

  if (done) {
    return (
      <AuthLayout title="Password updated" subtitle="You can now sign in with your new password.">
        <Button component={RouterLink} to="/login" variant="contained" fullWidth size="large">
          Go to sign in
        </Button>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout
      title="Reset password"
      subtitle="Use the one-time reset token an administrator issued for your account."
      footer={
        <Typography variant="body2">
          <Link component={RouterLink} to="/login">
            Back to sign in
          </Link>
        </Typography>
      }
    >
      <Stack component="form" spacing={2} onSubmit={submit} noValidate>
        {error && <Alert severity="error">{error.message}</Alert>}
        <TextField label="Reset token" value={form.reset_token} onChange={set('reset_token')} required />
        <TextField
          label="New password"
          type="password"
          value={form.new_password}
          onChange={set('new_password')}
          autoComplete="new-password"
          helperText="At least 8 characters."
          required
        />
        <TextField
          label="Confirm new password"
          type="password"
          value={form.confirm}
          onChange={set('confirm')}
          autoComplete="new-password"
          error={!!mismatch}
          helperText={mismatch ? 'Passwords do not match.' : ' '}
          required
        />
        <LoadingButton
          type="submit"
          variant="contained"
          size="large"
          loading={pending}
          disabled={!form.reset_token || !form.new_password || form.confirm !== form.new_password}
        >
          Set new password
        </LoadingButton>
      </Stack>
    </AuthLayout>
  )
}
