import { useRef, useState } from 'react'
import { Alert, Box, Button, Card, CardContent, CardHeader, Divider, Grid, MenuItem, Stack, TextField, Typography } from '@mui/material'
import FileDownloadOutlined from '@mui/icons-material/FileDownloadOutlined'
import FileUploadOutlined from '@mui/icons-material/FileUploadOutlined'
import ContentCopyOutlined from '@mui/icons-material/ContentCopyOutlined'
import { api } from '../../api/endpoints'
import { useAction, useColonies } from '../../api/queries'
import { useSession } from '../../auth/SessionProvider'
import { canManageColony, hasRole } from '../../auth/permissions'
import PageHeader from '../../components/PageHeader'
import { LoadingButton } from '../../components/LoadingButton'
import { useNotify } from '../../components/Notifications'
import { fontMono } from '../../app/theme'

const MAX_BUNDLE_BYTES = 8 << 20 // server limit for /relay/import

function ColonySelect({ label, value, onChange, options }) {
  return (
    <TextField select label={label} value={value} onChange={(e) => onChange(e.target.value)}>
      {options.map((c) => (
        <MenuItem key={c.id} value={c.id}>
          {c.name}
        </MenuItem>
      ))}
    </TextField>
  )
}

function JsonBox({ slotProps, ...props }) {
  return (
    <TextField
      multiline
      minRows={12}
      maxRows={22}
      {...props}
      slotProps={{
        ...slotProps,
        htmlInput: { spellCheck: false, style: { fontFamily: fontMono, fontSize: 12, lineHeight: 1.5 }, ...slotProps?.htmlInput },
      }}
    />
  )
}

function ExportCard({ colonies, sources }) {
  const notify = useNotify()
  const [from, setFrom] = useState(sources.length === 1 ? sources[0].id : '')
  const [to, setTo] = useState('')
  const [bundle, setBundle] = useState(null)
  const exp = useAction(() => api.exportBundle({ colony_id: from, to_colony_id: to }), {
    onSuccess: (b) => {
      setBundle(b)
      notify.success(`Exported ${b?.messages?.length ?? 0} message(s).`)
    },
  })
  const text = bundle ? JSON.stringify(bundle, null, 2) : ''
  const download = () => {
    const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }))
    const a = Object.assign(document.createElement('a'), { href: url, download: `${bundle.id || 'bundle'}.json` })
    a.click()
    URL.revokeObjectURL(url)
  }
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      notify.info('Bundle copied to clipboard.')
    } catch {
      notify.error('Copy failed; select the text and copy it manually.')
    }
  }
  return (
    <Card sx={{ height: '100%' }}>
      <CardHeader title="Export outbound bundle" subheader="Collect signed envelopes waiting to leave a colony." />
      <Divider />
      <CardContent>
        <Stack spacing={2}>
          {exp.error && <Alert severity="error">{exp.error.message}</Alert>}
          <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
            <ColonySelect label="From colony" value={from} onChange={setFrom} options={sources} />
            <ColonySelect label="To colony" value={to} onChange={setTo} options={colonies.filter((c) => c.id !== from)} />
          </Stack>
          <LoadingButton variant="contained" loading={exp.isPending} disabled={!from || !to} onClick={() => exp.mutate()} startIcon={<FileUploadOutlined />}>
            Generate bundle
          </LoadingButton>
          {bundle && (
            <>
              <Typography variant="body2" color="text.secondary">
                {bundle.messages?.length ?? 0} message(s). Carry this file to the destination colony and import it there.
              </Typography>
              <JsonBox value={text} slotProps={{ input: { readOnly: true } }} />
              <Stack direction="row" spacing={1}>
                <Button variant="outlined" startIcon={<FileDownloadOutlined />} onClick={download}>
                  Download
                </Button>
                <Button variant="outlined" startIcon={<ContentCopyOutlined />} onClick={copy}>
                  Copy
                </Button>
              </Stack>
            </>
          )}
        </Stack>
      </CardContent>
    </Card>
  )
}

function ImportCard({ targets }) {
  const notify = useNotify()
  const fileRef = useRef(null)
  const [target, setTarget] = useState(targets.length === 1 ? targets[0].id : '')
  const [text, setText] = useState('')
  const [parseError, setParseError] = useState('')
  const [result, setResult] = useState(null)

  const imp = useAction(
    () => {
      const bundle = JSON.parse(text)
      return api.importBundle({ colony_id: target, bundle })
    },
    {
      onSuccess: (r) => {
        setResult(r)
        setText('')
        notify.success(`Imported ${r?.imported_count ?? 0} message(s).`)
      },
    },
  )

  const onText = (v) => {
    setText(v)
    setResult(null)
    if (!v.trim()) return setParseError('')
    try {
      const b = JSON.parse(v)
      setParseError(Array.isArray(b?.messages) ? '' : 'Bundle must contain a "messages" array.')
    } catch {
      setParseError('Not valid JSON.')
    }
  }
  const onFile = async (e) => {
    const f = e.target.files?.[0]
    e.target.value = ''
    if (!f) return
    if (f.size > MAX_BUNDLE_BYTES) return notify.error('Bundle is larger than the 8 MiB import limit.')
    onText(await f.text())
  }

  return (
    <Card sx={{ height: '100%' }}>
      <CardHeader title="Import inbound bundle" subheader="Verify and apply envelopes delivered from another colony." />
      <Divider />
      <CardContent>
        <Stack spacing={2}>
          {imp.error && <Alert severity="error">{imp.error.message}</Alert>}
          {result && (
            <Alert severity="success">
              Imported {result.imported_count} message(s)
              {result.skipped_duplicates ? `, skipped ${result.skipped_duplicates} duplicate(s)` : ''}
              {result.generated_outbox ? `; ${result.generated_outbox} reply message(s) are now waiting for export` : ''}.
            </Alert>
          )}
          <ColonySelect label="Target colony" value={target} onChange={setTarget} options={targets} />
          <JsonBox
            label="Bundle JSON"
            placeholder='{"id": "...", "messages": [...]}'
            value={text}
            onChange={(e) => onText(e.target.value)}
            error={!!parseError}
            helperText={parseError || ' '}
          />
          <Stack direction="row" spacing={1}>
            <input ref={fileRef} type="file" accept="application/json,.json" hidden onChange={onFile} />
            <Button variant="outlined" startIcon={<FileUploadOutlined />} onClick={() => fileRef.current?.click()}>
              Load file
            </Button>
            <Box sx={{ flex: 1 }} />
            <LoadingButton variant="contained" loading={imp.isPending} disabled={!target || !text.trim() || !!parseError} onClick={() => imp.mutate()}>
              Import bundle
            </LoadingButton>
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  )
}

export default function RelayPage() {
  const { user } = useSession()
  const colonies = useColonies()
  const all = colonies.data ?? []
  // Relay operators and colony admins act for their own colony; super admins for any.
  const mine = all.filter((c) => canManageColony(user, c.id) || (hasRole(user, 'relay_operator') && user?.colony_id === c.id))

  return (
    <>
      <PageHeader
        title="Relay"
        description="There is no real-time link between colonies: messages travel as signed bundles that operators export and import."
      />
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <ExportCard colonies={all} sources={mine} />
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <ImportCard targets={mine} />
        </Grid>
      </Grid>
    </>
  )
}
