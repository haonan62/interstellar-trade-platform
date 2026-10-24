import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Avatar, Box, ButtonBase, Divider, ListItemIcon, Menu, MenuItem, Typography } from '@mui/material'
import LogoutOutlined from '@mui/icons-material/LogoutOutlined'
import ShieldOutlined from '@mui/icons-material/ShieldOutlined'
import { useSession } from '../../auth/SessionProvider'
import { useColonies } from '../../api/queries'
import { initials } from '../../lib/format'
import { RoleChips } from '../Chips'

export default function UserMenu() {
  const { user, signOut } = useSession()
  const colonies = useColonies()
  const navigate = useNavigate()
  const [anchor, setAnchor] = useState(null)
  const name = user?.display_name || user?.username || 'User'
  const colony = colonies.data?.find((c) => c.id === user?.colony_id)

  return (
    <>
      <ButtonBase
        onClick={(e) => setAnchor(e.currentTarget)}
        aria-label="Account menu"
        sx={{ borderRadius: 2, px: 1, py: 0.5, gap: 1.25, '&:hover': { bgcolor: 'action.hover' } }}
      >
        <Avatar sx={{ width: 30, height: 30, fontSize: '0.75rem', fontWeight: 600, bgcolor: 'primary.main' }}>{initials(name)}</Avatar>
        <Box sx={{ textAlign: 'left', display: { xs: 'none', md: 'block' }, lineHeight: 1.2 }}>
          <Typography variant="subtitle2" sx={{ lineHeight: 1.2 }}>
            {name}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {colony?.name ?? 'Platform'}
          </Typography>
        </Box>
      </ButtonBase>
      <Menu
        anchorEl={anchor}
        open={!!anchor}
        onClose={() => setAnchor(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        slotProps={{ paper: { variant: 'outlined', sx: { mt: 1, minWidth: 240 } } }}
      >
        <Box sx={{ px: 2, py: 1.5 }}>
          <Typography variant="subtitle2">{name}</Typography>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
            @{user?.username}
          </Typography>
          <RoleChips roles={user?.roles} />
        </Box>
        <Divider />
        <MenuItem
          onClick={() => {
            setAnchor(null)
            navigate('/settings/security')
          }}
        >
          <ListItemIcon>
            <ShieldOutlined fontSize="small" />
          </ListItemIcon>
          Security settings
        </MenuItem>
        <MenuItem
          onClick={async () => {
            setAnchor(null)
            await signOut()
            navigate('/login')
          }}
        >
          <ListItemIcon>
            <LogoutOutlined fontSize="small" />
          </ListItemIcon>
          Sign out
        </MenuItem>
      </Menu>
    </>
  )
}
