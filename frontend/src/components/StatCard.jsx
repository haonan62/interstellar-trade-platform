import { Box, Card, CardContent, Skeleton, Stack, Typography } from '@mui/material'
import { alpha } from '@mui/material/styles'

export default function StatCard({ label, value, icon: Icon, hint, tone = 'primary', loading }) {
  return (
    <Card sx={{ height: '100%' }}>
      <CardContent>
        <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <Box>
            <Typography variant="overline" color="text.secondary">
              {label}
            </Typography>
            <Typography sx={{ fontSize: '1.75rem', fontWeight: 650, letterSpacing: '-0.02em', lineHeight: 1.2, mt: 0.5 }}>
              {loading ? <Skeleton width={56} /> : value}
            </Typography>
            {hint && (
              <Typography variant="caption" color="text.secondary">
                {hint}
              </Typography>
            )}
          </Box>
          {Icon && (
            <Box
              sx={(t) => ({
                width: 36,
                height: 36,
                borderRadius: 2,
                display: 'grid',
                placeItems: 'center',
                color: t.palette[tone].main,
                bgcolor: alpha(t.palette[tone].main, 0.1),
              })}
            >
              <Icon fontSize="small" />
            </Box>
          )}
        </Stack>
      </CardContent>
    </Card>
  )
}
