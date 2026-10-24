import { json, request } from './client'

const post = (path, body, headers) => json(path, { method: 'POST', body, headers })
const id = encodeURIComponent

// One function per backend route. Paths mirror backend/internal/httpapi.
export const api = {
  // auth
  needsBootstrap: () => json('/api/v1/bootstrap'),
  bootstrap: (body, bootstrapToken) => post('/api/v1/auth/bootstrap', body, { 'X-Bootstrap-Token': bootstrapToken }),
  login: (body) => post('/api/v1/auth/login', body),
  logout: () => post('/api/v1/auth/logout'),
  me: () => json('/api/v1/auth/me'),
  changePassword: (body) => post('/api/v1/auth/password', body),
  completeReset: (body) => post('/api/v1/auth/reset-password', body),
  enrollMFA: () => post('/api/v1/auth/mfa/enroll'),
  confirmMFA: (code) => post('/api/v1/auth/mfa/confirm', { code }),

  // overview
  dashboard: () => json('/api/v1/dashboard'),

  // colonies
  colonies: () => json('/api/v1/colonies'),
  createColony: (body) => post('/api/v1/colonies', body),
  trustPeer: (colonyId, peerColonyId) => post(`/api/v1/colonies/${id(colonyId)}/trust`, { peer_colony_id: peerColonyId }),
  // 200 = applied, 202 = proposed (dual control)
  setPolicy: (colonyId, body) => request(`/api/v1/colonies/${id(colonyId)}/policy`, { method: 'POST', body }),

  // users
  users: () => json('/api/v1/users'),
  createUser: (body) => post('/api/v1/users', body),
  setUserActive: (userId, active) => post(`/api/v1/users/${id(userId)}/active`, { active }),
  setUserRoles: (userId, roles) => post(`/api/v1/users/${id(userId)}/roles`, { roles }),
  issueReset: (userId) => post(`/api/v1/users/${id(userId)}/reset-password`),
  resetMFA: (userId) => post(`/api/v1/users/${id(userId)}/mfa/reset`),

  // accounts
  accounts: () => json('/api/v1/accounts'),
  // 200 = minted, 202 = proposed (dual control)
  mint: (body) => request('/api/v1/accounts/mint', { method: 'POST', body }),

  // trades
  trades: () => json('/api/v1/trades'),
  createOffer: (body) => post('/api/v1/trades/offers', body),
  acceptTrade: (tradeId) => post(`/api/v1/trades/${id(tradeId)}/accept`),
  cancelTrade: (tradeId) => post(`/api/v1/trades/${id(tradeId)}/cancel`),
  forceRefund: (tradeId) => post(`/api/v1/trades/${id(tradeId)}/force-refund`),

  // approvals (dual control)
  approvals: () => json('/api/v1/approvals'),
  approve: (actionId) => post(`/api/v1/approvals/${id(actionId)}/approve`),
  reject: (actionId) => post(`/api/v1/approvals/${id(actionId)}/reject`),

  // relay
  exportBundle: (body) => post('/api/v1/relay/export', body),
  importBundle: (body) => post('/api/v1/relay/import', body),

  // ledger
  ledger: (colonyId, { limit = 200, beforeSeq } = {}) => {
    const q = new URLSearchParams({ colony_id: colonyId, limit: String(limit) })
    if (beforeSeq) q.set('before_seq', String(beforeSeq))
    return json(`/api/v1/ledger?${q}`)
  },
}
