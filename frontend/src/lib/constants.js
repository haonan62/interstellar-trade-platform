// Values mirror backend/internal/domain (trade_state.go, model.go).

export const ROLES = ['super_admin', 'colony_admin', 'trader', 'relay_operator']

export const ROLE_META = {
  super_admin: { label: 'Super admin', color: 'warning', description: 'Full platform control' },
  colony_admin: { label: 'Colony admin', color: 'primary', description: 'Manages one colony' },
  trader: { label: 'Trader', color: 'success', description: 'Creates and accepts offers' },
  relay_operator: { label: 'Relay operator', color: 'info', description: 'Moves bundles between colonies' },
}

// tone: MUI palette key used by StatusChip. step: position on the happy path.
export const TRADE_STATUS = {
  offer_pending_export: { label: 'Offer pending export', tone: 'default', step: 0 },
  offer_received: { label: 'Awaiting acceptance', tone: 'warning', step: 1 },
  acceptance_pending_export: { label: 'Accepted · in escrow', tone: 'info', step: 2 },
  settlement_pending_export: { label: 'Settling', tone: 'info', step: 3 },
  completed: { label: 'Completed', tone: 'success', step: 4 },
  refund_pending_export: { label: 'Refund pending', tone: 'warning' },
  refunded: { label: 'Refunded', tone: 'default' },
  expired: { label: 'Expired', tone: 'default' },
  cancelled: { label: 'Cancelled', tone: 'error' },
}

export const OPEN_TRADE_STATUSES = new Set([
  'offer_pending_export',
  'offer_received',
  'acceptance_pending_export',
  'settlement_pending_export',
  'refund_pending_export',
])

export const APPROVAL_TYPE = {
  mint: 'Mint funds',
  force_refund: 'Force refund',
  policy: 'Policy change',
}

export const APPROVAL_STATUS = {
  pending: { label: 'Pending', tone: 'warning' },
  approved: { label: 'Approved', tone: 'success' },
  rejected: { label: 'Rejected', tone: 'error' },
}
