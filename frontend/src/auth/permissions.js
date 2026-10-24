// Client-side role checks decide what to show; the server enforces every
// rule again, so these only need to be helpful, not authoritative.

export const hasRole = (user, role) => !!user?.roles?.includes(role)
export const isSuperAdmin = (user) => hasRole(user, 'super_admin')
export const isAdmin = (user) => isSuperAdmin(user) || hasRole(user, 'colony_admin')
export const canRelay = (user) => isAdmin(user) || hasRole(user, 'relay_operator')
export const canTrade = (user) => isSuperAdmin(user) || hasRole(user, 'trader')

// canManageColony mirrors service.canManageColony.
export const canManageColony = (user, colonyId) =>
  isSuperAdmin(user) || (hasRole(user, 'colony_admin') && user?.colony_id === colonyId)
