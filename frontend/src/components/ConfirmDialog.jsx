import FormDialog from './FormDialog'

export default function ConfirmDialog({ open, onClose, title, message, confirmLabel = 'Confirm', danger, onConfirm, pending, error }) {
  return (
    <FormDialog
      open={open}
      onClose={onClose}
      title={title}
      description={message}
      submitLabel={confirmLabel}
      submitColor={danger ? 'error' : 'primary'}
      onSubmit={onConfirm}
      pending={pending}
      error={error}
    />
  )
}
