import { Alert, Button } from '@mui/material'

// QueryError shows a failed load with a retry button.
export function QueryError({ error, onRetry }) {
  if (!error) return null
  return (
    <Alert
      severity="error"
      sx={{ mb: 2 }}
      action={
        onRetry && (
          <Button color="inherit" size="small" onClick={() => onRetry()}>
            Retry
          </Button>
        )
      }
    >
      {error.message}
    </Alert>
  )
}
