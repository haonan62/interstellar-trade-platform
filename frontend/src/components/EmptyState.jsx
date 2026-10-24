import { Box, Typography } from '@mui/material'
import InboxOutlined from '@mui/icons-material/InboxOutlined'

export default function EmptyState({ icon: Icon = InboxOutlined, title, description, action, dense }) {
  return (
    <Box sx={{ textAlign: 'center', py: dense ? 4 : 8, px: 3 }}>
      <Box
        sx={{
          width: 44,
          height: 44,
          mx: 'auto',
          mb: 1.5,
          borderRadius: '50%',
          display: 'grid',
          placeItems: 'center',
          bgcolor: 'action.hover',
          color: 'text.secondary',
        }}
      >
        <Icon fontSize="small" />
      </Box>
      <Typography variant="subtitle1">{title}</Typography>
      {description && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, maxWidth: 420, mx: 'auto' }}>
          {description}
        </Typography>
      )}
      {action && <Box sx={{ mt: 2 }}>{action}</Box>}
    </Box>
  )
}
