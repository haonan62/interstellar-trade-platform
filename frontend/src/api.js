const API_ROOT = "";

async function request(path, { method = "GET", token, body } = {}) {
  const headers = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(`${API_ROOT}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text ? { message: text } : null;
  }
  if (!res.ok) {
    throw new Error(data?.message || `Request failed: ${res.status}`);
  }
  return data;
}

export const api = {
  needsBootstrap: () => request("/api/v1/bootstrap"),
  bootstrap: (body) =>
    request("/api/v1/auth/bootstrap", { method: "POST", body }),
  login: (body) => request("/api/v1/auth/login", { method: "POST", body }),
  logout: (token) => request("/api/v1/auth/logout", { method: "POST", token }),
  me: (token) => request("/api/v1/auth/me", { token }),
  dashboard: (token) => request("/api/v1/dashboard", { token }),
  colonies: (token) => request("/api/v1/colonies", { token }),
  createColony: (token, body) =>
    request("/api/v1/colonies", { method: "POST", token, body }),
  trustPeer: (token, colonyId, body) =>
    request(`/api/v1/colonies/${colonyId}/trust`, {
      method: "POST",
      token,
      body,
    }),
  users: (token) => request("/api/v1/users", { token }),
  createUser: (token, body) =>
    request("/api/v1/users", { method: "POST", token, body }),
  accounts: (token) => request("/api/v1/accounts", { token }),
  mint: (token, body) =>
    request("/api/v1/accounts/mint", { method: "POST", token, body }),
  trades: (token) => request("/api/v1/trades", { token }),
  createOffer: (token, body) =>
    request("/api/v1/trades/offers", { method: "POST", token, body }),
  acceptTrade: (token, tradeId) =>
    request(`/api/v1/trades/${tradeId}/accept`, { method: "POST", token }),
  exportBundle: (token, body) =>
    request("/api/v1/relay/export", { method: "POST", token, body }),
  importBundle: (token, body) =>
    request("/api/v1/relay/import", { method: "POST", token, body }),
  ledger: (token, colonyId, limit = 200) =>
    request(
      `/api/v1/ledger?colony_id=${encodeURIComponent(colonyId)}&limit=${limit}`,
      { token },
    ),
};
