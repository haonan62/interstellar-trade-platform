import { useState } from 'react'
import { Link as RouterLink, useLocation, useNavigate } from 'react-router-dom'
import { Alert, Link, Stack, TextField, Typography } from '@mui/material'
import { useSession } from '../../auth/SessionProvider'
import { LoadingButton } from '../../components/LoadingButton'
import AuthLayout from './AuthLayout'

export default function LoginPage() {
  const { signIn } = useSession()
  const navigate = useNavigate()
  const location = useLocation()
  const [form, setForm] = useState({ username: '', password: '', totp_code: '' })
  const [needsCode, setNeedsCode] = useState(false)
  const [error, setError] = useState(null)
  const [pending, setPending] = useState(false)

  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value })

  const submit = async (e) => {
    e.preventDefault()
    setPending(true)
    setError(null)
    try {
      const { totp_code, ...rest } = form
      await signIn(totp_code ? { ...rest, totp_code } : rest)
      navigate(location.state?.from ?? '/', { replace: true })
    } catch (err) {
      if (err.code === 'mfa_required' && !needsCode) {
        setNeedsCode(true)
      } else {
        setError(err)
      }
    } finally {
      setPending(false)
    }
  }

  return (
    <AuthLayout
      title={needsCode ? 'Two-factor authentication' : 'Sign in'}
      subtitle={
        needsCode
          ? 'Enter the 6-digit code from your authenticator app.'
          : 'Access colony administration, trading and relay operations.'
      }
      footer={
        <Typography variant="body2" color="text.secondary">
          Have a reset token?{' '}
          <Link component={RouterLink} to="/reset-password">
            Reset your password
          </Link>
        </Typography>
      }
    >
      <Stack component="form" spacing={2} onSubmit={submit} noValidate>
        {error && <Alert severity="error">{error.message}</Alert>}
        {!needsCode ? (
          <>
            <TextField label="Username" value={form.username} onChange={set('username')} autoComplete="username" autoFocus required />
            <TextField
              label="Password"
              type="password"
              value={form.password}
              onChange={set('password')}
              autoComplete="current-password"
              required
            />
          </>
        ) : (
          <TextField
            label="Authentication code"
            value={form.totp_code}
            onChange={(e) => setForm({ ...form, totp_code: e.target.value.replace(/\D/g, '').slice(0, 6) })}
            autoComplete="one-time-code"
            slotProps={{ htmlInput: { inputMode: 'numeric', pattern: '[0-9]*', style: { letterSpacing: '0.3em' } } }}
            autoFocus
          />
        )}
        <LoadingButton
          type="submit"
          variant="contained"
          size="large"
          loading={pending}
          disabled={!form.username || !form.password || (needsCode && form.totp_code.length !== 6)}
        >
          {needsCode ? 'Verify' : 'Sign in'}
        </LoadingButton>
        {needsCode && (
          <Link
            component="button"
            type="button"
            variant="body2"
            onClick={() => {
              setNeedsCode(false)
              setForm({ ...form, totp_code: '' })
              setError(null)
            }}
          >
            Back
          </Link>
        )}
      </Stack>
    </AuthLayout>
  )
}
