// Low-level HTTP client for the /api/v1 REST API.
//
// Sessions are an HttpOnly cookie set by the server when a request carries
// X-Session-Mode: cookie; JavaScript never sees the token, so an XSS bug
// cannot read it from storage. The browser sends the cookie on same-origin
// requests and the server checks Origin on every cookie-authenticated POST.

export class ApiError extends Error {
  constructor(message, { status, code } = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

const listeners = new Set()

// onApiError lets the session layer react to 401 (signed out elsewhere or
// expired) and to the admin-MFA gate without every caller handling them.
export function onApiError(listener) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export async function request(path, { method = 'GET', body, headers: extra, signal } = {}) {
  const headers = { 'X-Session-Mode': 'cookie', Accept: 'application/json', ...extra }
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  let res
  try {
    res = await fetch(path, {
      method,
      headers,
      credentials: 'same-origin',
      body: body !== undefined ? JSON.stringify(body) : undefined,
      signal,
    })
  } catch (err) {
    if (err?.name === 'AbortError') throw err
    throw new ApiError('Unable to reach the server. Check your connection and try again.', { status: 0, code: 'network_error' })
  }

  const text = await res.text()
  let data = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { message: text }
    }
  }
  if (!res.ok) {
    const error = new ApiError(data?.message || `Request failed (${res.status})`, { status: res.status, code: data?.code })
    listeners.forEach((l) => l(error))
    throw error
  }
  return { status: res.status, data }
}

// json is request() for callers that only need the body.
export const json = async (path, options) => (await request(path, options)).data
