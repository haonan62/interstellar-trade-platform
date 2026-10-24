import { Chip, Stack } from '@mui/material'
import { alpha } from '@mui/material/styles'
import { APPROVAL_STATUS, ROLE_META, TRADE_STATUS } from '../lib/constants'
import { humanize } from '../lib/format'

// SoftChip is a tinted chip that reads well in both color schemes.
export function SoftChip({ label, tone = 'default', ...rest }) {
  return (
    <Chip
      size="small"
      label={label}
      sx={(theme) => {
        if (tone === 'default') {
          return { bgcolor: 'action.hover', color: 'text.secondary', border: 0 }
        }
        const c = theme.palette[tone].main
        return {
          bgcolor: alpha(c, 0.12),
          color: c,
          border: `1px solid ${alpha(c, 0.24)}`,
          ...theme.applyStyles('dark', { bgcolor: alpha(c, 0.16) }),
        }
      }}
      {...rest}
    />
  )
}

export function TradeStatusChip({ status }) {
  const meta = TRADE_STATUS[status] ?? { label: humanize(status), tone: 'default' }
  return <SoftChip label={meta.label} tone={meta.tone} />
}

export function ApprovalStatusChip({ status }) {
  const meta = APPROVAL_STATUS[status] ?? { label: humanize(status), tone: 'default' }
  return <SoftChip label={meta.label} tone={meta.tone} />
}

export function RoleChips({ roles = [] }) {
  return (
    <Stack direction="row" spacing={0.5} sx={{ flexWrap: 'wrap', rowGap: 0.5 }}>
      {roles.map((r) => (
        <SoftChip key={r} label={ROLE_META[r]?.label ?? humanize(r)} tone={ROLE_META[r]?.color ?? 'default'} />
      ))}
    </Stack>
  )
}
