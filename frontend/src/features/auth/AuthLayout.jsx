import { Box, Card, CardContent, Stack, Typography } from '@mui/material'
import Logo from '../../components/layout/Logo'

export default function AuthLayout({ title, subtitle, children, footer }) {
  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        px: 2,
        py: 6,
        bgcolor: 'background.default',
        backgroundImage: (t) =>
          `radial-gradient(60rem 30rem at 50% -10%, rgba(${t.vars.palette.primary.mainChannel} / 0.12), transparent 70%)`,
      }}
    >
      <Box sx={{ width: '100%', maxWidth: 400 }}>
        <Stack sx={{ alignItems: 'center', mb: 4 }}>
          <Logo size={40} />
        </Stack>
        <Card sx={{ boxShadow: '0 1px 2px rgba(15,23,42,0.04), 0 12px 32px -12px rgba(15,23,42,0.12)' }}>
          <CardContent sx={{ p: { xs: 3, sm: 4 }, '&:last-child': { pb: { xs: 3, sm: 4 } } }}>
            <Typography variant="h5" component="h1">
              {title}
            </Typography>
            {subtitle && (
              <Typography variant="body2" color="text.secondary" sx={{ mt: 0.75, mb: 3 }}>
                {subtitle}
              </Typography>
            )}
            {children}
          </CardContent>
        </Card>
        {footer && <Box sx={{ mt: 3, textAlign: 'center' }}>{footer}</Box>}
      </Box>
    </Box>
  )
}
