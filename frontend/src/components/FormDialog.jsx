import { Alert, Button, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle, Stack } from '@mui/material'
import { LoadingButton } from './LoadingButton'

// FormDialog renders a modal form. onSubmit may throw; the error is shown
// inline so the user keeps what they typed.
export default function FormDialog({
  open,
  onClose,
  title,
  description,
  submitLabel = 'Save',
  submitColor = 'primary',
  onSubmit,
  pending,
  error,
  disabled,
  maxWidth = 'xs',
  children,
}) {
  const handleSubmit = (e) => {
    e.preventDefault()
    if (!disabled && !pending) onSubmit?.()
  }
  return (
    <Dialog open={open} onClose={pending ? undefined : onClose} fullWidth maxWidth={maxWidth}>
      <form onSubmit={handleSubmit} noValidate>
        <DialogTitle>{title}</DialogTitle>
        <DialogContent>
          {description && <DialogContentText sx={{ mb: 2, fontSize: '0.875rem' }}>{description}</DialogContentText>}
          <Stack spacing={2} sx={{ pt: description ? 0 : 1 }}>
            {error && <Alert severity="error">{error.message ?? String(error)}</Alert>}
            {children}
          </Stack>
        </DialogContent>
        <DialogActions>
          {onSubmit ? (
            <>
              <Button onClick={onClose} disabled={pending} color="inherit">
                Cancel
              </Button>
              <LoadingButton type="submit" variant="contained" color={submitColor} loading={pending} disabled={disabled}>
                {submitLabel}
              </LoadingButton>
            </>
          ) : (
            <Button onClick={onClose} variant="contained">
              Close
            </Button>
          )}
        </DialogActions>
      </form>
    </Dialog>
  )
}
