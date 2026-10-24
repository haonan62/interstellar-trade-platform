import { useEffect, useState } from 'react'
import { Alert, Box, Card, CardContent, CardHeader, Divider, Grid, Stack, TextField, Typography } from '@mui/material'
import VerifiedUserOutlined from '@mui/icons-material/VerifiedUserOutlined'
import QRCode from 'qrcode'
import { api } from '../../api/endpoints'
import { useSession } from '../../auth/SessionProvider'
import { LoadingButton } from '../../components/LoadingButton'
import Mono from '../../components/Mono'
import { SoftChip } from '../../components/Chips'
import PageHeader from '../../components/PageHeader'
import { useNotify } from '../../components/Notifications'

function ChangePasswordCard() {
  const notify = useNotify()
  const [form, setForm] = useState({ current_password: '', new_password: '', confirm: '' })
  const [error, setError] = useState(null)
  const [pending, setPending] = useState(false)
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.value })
  const mismatch = form.confirm && form.confirm !== form.new_password

  const submit = async (e) => {
    e.preventDefault()
    setPending(true)
    setError(null)
    try {
      await api.changePassword({ current_password: form.current_password, new_password: form.new_password })
      setForm({ current_password: '', new_password: '', confirm: '' })
      notify.success('Password changed. Other sessions were signed out.')
    } catch (err) {
      setError(err)
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader title="Password" subheader="Changing it signs out your other sessions." />
      <Divider />
      <CardContent>
        <Stack component="form" spacing={2} onSubmit={submit} noValidate>
          {error && <Alert severity="error">{error.message}</Alert>}
          <TextField label="Current password" type="password" value={form.current_password} onChange={set('current_password')} autoComplete="current-password" />
          <TextField
            label="New password"
            type="password"
            value={form.new_password}
            onChange={set('new_password')}
            autoComplete="new-password"
            helperText="At least 8 characters."
          />
          <TextField
            label="Confirm new password"
            type="password"
            value={form.confirm}
            onChange={set('confirm')}
            autoComplete="new-password"
            error={!!mismatch}
            helperText={mismatch ? 'Passwords do not match.' : ' '}
          />
          <Box>
            <LoadingButton
              type="submit"
              variant="contained"
              loading={pending}
              disabled={!form.current_password || form.new_password.length < 8 || form.new_password !== form.confirm}
            >
              Update password
            </LoadingButton>
          </Box>
        </Stack>
      </CardContent>
    </Card>
  )
}

function MfaCard() {
  const { user, refreshUser } = useSession()
  const notify = useNotify()
  const [enrollment, setEnrollment] = useState(null)
  const [qr, setQr] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState(null)
  const [pending, setPending] = useState(false)

  useEffect(() => {
    if (!enrollment?.otpauth_url) return
    QRCode.toDataURL(enrollment.otpauth_url, { margin: 1, width: 180 }).then(setQr, () => setQr(''))
  }, [enrollment])

  const start = async () => {
    setPending(true)
    setError(null)
    try {
      setEnrollment(await api.enrollMFA())
    } catch (err) {
      setError(err)
    } finally {
      setPending(false)
    }
  }
  const confirm = async (e) => {
    e.preventDefault()
    setPending(true)
    setError(null)
    try {
      await api.confirmMFA(code)
      setEnrollment(null)
      setCode('')
      await refreshUser()
      notify.success('Two-factor authentication is enabled.')
    } catch (err) {
      setError(err)
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader
        title="Two-factor authentication"
        subheader="Require a code from an authenticator app at sign-in."
        action={<SoftChip label={user?.mfa_enabled ? 'Enabled' : 'Off'} tone={user?.mfa_enabled ? 'success' : 'default'} />}
      />
      <Divider />
      <CardContent>
        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error.message}
          </Alert>
        )}
        {user?.mfa_enabled ? (
          <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
            <VerifiedUserOutlined color="success" />
            <Typography variant="body2">
              Your account is protected. If you lose your device, ask an administrator to reset MFA.
            </Typography>
          </Stack>
        ) : enrollment ? (
          <Stack component="form" spacing={2} onSubmit={confirm} noValidate>
            <Typography variant="body2">
              1. Scan this code with an authenticator app (1Password, Google Authenticator, Authy…).
            </Typography>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ alignItems: { sm: 'center' } }}>
              {qr && <Box component="img" src={qr} alt="MFA QR code" sx={{ width: 180, height: 180, borderRadius: 1, bgcolor: '#fff', p: 0.5 }} />}
              <Box sx={{ minWidth: 0 }}>
                <Typography variant="caption" color="text.secondary">
                  Or enter this secret manually
                </Typography>
                <Box sx={{ wordBreak: 'break-all' }}>
                  <Mono value={enrollment.secret} short={false} copy color="text.primary" />
                </Box>
              </Box>
            </Stack>
            <Typography variant="body2">2. Enter the 6-digit code it shows.</Typography>
            <TextField
              label="Authentication code"
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
              autoComplete="one-time-code"
              sx={{ maxWidth: 220 }}
              slotProps={{ htmlInput: { inputMode: 'numeric', style: { letterSpacing: '0.3em' } } }}
            />
            <Box>
              <LoadingButton type="submit" variant="contained" loading={pending} disabled={code.length !== 6}>
                Verify and enable
              </LoadingButton>
            </Box>
          </Stack>
        ) : (
          <LoadingButton variant="contained" loading={pending} onClick={start}>
            Set up two-factor authentication
          </LoadingButton>
        )}
      </CardContent>
    </Card>
  )
}

export default function SecurityPage() {
  const { mfaEnrollmentRequired } = useSession()
  return (
    <>
      <PageHeader title="Security" description="Manage how you sign in." />
      {mfaEnrollmentRequired && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          This platform requires administrators to use two-factor authentication. Set it up below to continue.
        </Alert>
      )}
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <MfaCard />
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <ChangePasswordCard />
        </Grid>
      </Grid>
    </>
  )
}
