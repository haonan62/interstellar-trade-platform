import { NavLink } from 'react-router-dom'
import { Badge, Box, List, ListItemButton, ListItemIcon, ListItemText, Typography } from '@mui/material'
import ShieldOutlined from '@mui/icons-material/ShieldOutlined'
import { NAV_SECTIONS } from '../../app/navigation'
import { useSession } from '../../auth/SessionProvider'
import { isAdmin } from '../../auth/permissions'
import { useApprovals } from '../../api/queries'
import Logo from './Logo'

function NavItem({ item, badgeCount, onNavigate }) {
  const Icon = item.icon
  return (
    <ListItemButton component={NavLink} to={item.to} end={item.end} onClick={onNavigate} sx={{ py: 0.75, px: 1.25 }}>
      <ListItemIcon>
        <Badge color="warning" badgeContent={badgeCount} max={99} invisible={!badgeCount}>
          <Icon fontSize="small" />
        </Badge>
      </ListItemIcon>
      <ListItemText primary={item.label} slotProps={{ primary: { sx: { fontSize: '0.875rem', fontWeight: 500 } } }} />
    </ListItemButton>
  )
}

export default function Sidebar({ onNavigate }) {
  const { user } = useSession()
  const approvals = useApprovals(isAdmin(user))
  const pending = (approvals.data ?? []).filter((a) => a.status === 'pending').length
  const badges = { approvals: pending }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ px: 2.5, height: 64, display: 'flex', alignItems: 'center' }}>
        <Logo />
      </Box>
      <Box
        component="nav"
        aria-label="Main"
        sx={{
          flex: 1,
          overflowY: 'auto',
          px: 1.5,
          pb: 2,
          // NavLink marks the active route with aria-current="page".
          '& a[aria-current="page"]': (theme) => ({
            bgcolor: `rgba(${theme.vars.palette.primary.mainChannel} / 0.1)`,
            color: 'primary.main',
            '& .MuiListItemIcon-root': { color: 'primary.main' },
          }),
        }}
      >
        {NAV_SECTIONS.map((section) => {
          const items = section.items.filter((i) => !i.visible || i.visible(user))
          if (!items.length) return null
          return (
            <Box key={section.title} sx={{ mt: 2 }}>
              <Typography variant="overline" color="text.secondary" sx={{ px: 1.25, display: 'block', mb: 0.5 }}>
                {section.title}
              </Typography>
              <List disablePadding>
                {items.map((item) => (
                  <NavItem key={item.to} item={item} badgeCount={item.badge ? badges[item.badge] : 0} onNavigate={onNavigate} />
                ))}
              </List>
            </Box>
          )
        })}
      </Box>
      <Box sx={{ px: 1.5, py: 1.5, borderTop: 1, borderColor: 'divider' }}>
        <NavItem item={{ to: '/settings/security', label: 'Security', icon: ShieldOutlined }} onNavigate={onNavigate} />
      </Box>
    </Box>
  )
}
