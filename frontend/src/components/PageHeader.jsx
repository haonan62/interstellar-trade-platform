import { Box, Stack, Typography } from '@mui/material'

export default function PageHeader({ title, description, actions }) {
  return (
    <Stack
      direction={{ xs: 'column', sm: 'row' }}
      spacing={2}
      sx={{ mb: 3, justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'flex-end' } }}
    >
      <Box sx={{ minWidth: 0 }}>
        <Typography variant="h4" component="h1">
          {title}
        </Typography>
        {description && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, maxWidth: 720 }}>
            {description}
          </Typography>
        )}
      </Box>
      {actions && (
        <Stack direction="row" spacing={1} sx={{ flexShrink: 0 }}>
          {actions}
        </Stack>
      )}
    </Stack>
  )
}
