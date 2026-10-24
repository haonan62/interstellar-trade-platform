import { Box, Stack, Typography } from '@mui/material'

export default function Logo({ size = 32, subtitle = true }) {
  return (
    <Stack direction="row" spacing={1.25} sx={{ alignItems: 'center' }}>
      <Box
        aria-hidden
        sx={{
          width: size,
          height: size,
          borderRadius: 2,
          display: 'grid',
          placeItems: 'center',
          color: '#fff',
          background: 'linear-gradient(135deg, #4f46e5 0%, #0ea5e9 100%)',
          flexShrink: 0,
        }}
      >
        <svg width={size * 0.56} height={size * 0.56} viewBox="0 0 24 24" fill="none">
          <circle cx="12" cy="12" r="4" fill="currentColor" />
          <ellipse cx="12" cy="12" rx="10" ry="4.2" stroke="currentColor" strokeWidth="1.6" transform="rotate(-25 12 12)" />
        </svg>
      </Box>
      <Box sx={{ lineHeight: 1.1 }}>
        <Typography sx={{ fontWeight: 700, fontSize: '0.9375rem', letterSpacing: '-0.01em' }}>Interstellar</Typography>
        {subtitle && (
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', lineHeight: 1.2 }}>
            Trade Platform
          </Typography>
        )}
      </Box>
    </Stack>
  )
}
