import DashboardOutlined from '@mui/icons-material/DashboardOutlined'
import SwapHorizOutlined from '@mui/icons-material/SwapHorizOutlined'
import AccountBalanceWalletOutlined from '@mui/icons-material/AccountBalanceWalletOutlined'
import ReceiptLongOutlined from '@mui/icons-material/ReceiptLongOutlined'
import SatelliteAltOutlined from '@mui/icons-material/SatelliteAltOutlined'
import FactCheckOutlined from '@mui/icons-material/FactCheckOutlined'
import PublicOutlined from '@mui/icons-material/PublicOutlined'
import PeopleOutlined from '@mui/icons-material/PeopleOutlined'
import { canRelay, isAdmin } from '../auth/permissions'

// Sidebar sections. `visible` hides entries the user cannot use; the server
// still authorizes every request.
export const NAV_SECTIONS = [
  {
    title: 'Overview',
    items: [{ to: '/', label: 'Dashboard', icon: DashboardOutlined, end: true }],
  },
  {
    title: 'Trading',
    items: [
      { to: '/trades', label: 'Trades', icon: SwapHorizOutlined },
      { to: '/accounts', label: 'Accounts', icon: AccountBalanceWalletOutlined },
      { to: '/ledger', label: 'Ledger', icon: ReceiptLongOutlined },
    ],
  },
  {
    title: 'Operations',
    items: [
      { to: '/relay', label: 'Relay', icon: SatelliteAltOutlined, visible: canRelay },
      { to: '/approvals', label: 'Approvals', icon: FactCheckOutlined, visible: isAdmin, badge: 'approvals' },
    ],
  },
  {
    title: 'Administration',
    items: [
      { to: '/colonies', label: 'Colonies', icon: PublicOutlined },
      { to: '/users', label: 'Users', icon: PeopleOutlined, visible: isAdmin },
    ],
  },
]
