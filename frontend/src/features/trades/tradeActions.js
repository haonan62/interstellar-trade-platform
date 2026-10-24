import { canManageColony, hasRole, isSuperAdmin } from '../../auth/permissions'

// Which actions the server would allow; mirrors service.AcceptTrade,
// service.CancelTrade and service.ProposeForceRefund.
export function tradeActions(user, t) {
  const trader = hasRole(user, 'trader')
  const superAdmin = isSuperAdmin(user)
  return {
    accept: trader && t.status === 'offer_received' && t.buyer_user_id === user?.id,
    withdraw:
      (trader || superAdmin) &&
      (t.status === 'offer_pending_export' || t.status === 'offer_received') &&
      (t.seller_user_id === user?.id || superAdmin),
    requestRefund: (trader || superAdmin) && t.status === 'acceptance_pending_export' && (t.buyer_user_id === user?.id || superAdmin),
    forceRefund:
      (t.status === 'acceptance_pending_export' || t.status === 'refund_pending_export') && canManageColony(user, t.buyer_colony_id),
  }
}
