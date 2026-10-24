import { Button } from '@mui/material'

// MUI v7 Button supports `loading` natively; this keeps one import site.
export function LoadingButton({ loading, children, ...rest }) {
  return (
    <Button loading={!!loading} loadingPosition={rest.startIcon ? 'start' : 'center'} {...rest}>
      {children}
    </Button>
  )
}
