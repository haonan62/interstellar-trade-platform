import { Component } from 'react'
import { Box, Button, Typography } from '@mui/material'

// ErrorBoundary keeps a rendering bug in one page from blanking the app.
export default class ErrorBoundary extends Component {
  state = { error: null }

  static getDerivedStateFromError(error) {
    return { error }
  }

  componentDidCatch(error, info) {
    console.error('UI error', error, info?.componentStack)
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <Box sx={{ p: 6, textAlign: 'center' }}>
        <Typography variant="h5" gutterBottom>
          Something went wrong
        </Typography>
        <Typography color="text.secondary" sx={{ mb: 3 }}>
          This page failed to render. Reloading usually helps; if not, contact an administrator.
        </Typography>
        <Button variant="contained" onClick={() => window.location.reload()}>
          Reload
        </Button>
      </Box>
    )
  }
}
