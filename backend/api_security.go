package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const sessionCookie = "itp_session"

// WithBootstrapToken sets the one-time secret that POST /auth/bootstrap must
// present in the X-Bootstrap-Token header. With no token configured, bootstrap
// is refused (fail closed) instead of being open to the first caller.
func (a *API) WithBootstrapToken(t string) *API { a.bootstrapToken = t; return a }

// MetricsHandler serves the Prometheus exposition. It is mounted on a separate
// listener (see --metrics-addr) and never on the public mux.
func (a *API) MetricsHandler() http.Handler { return a.metrics }

func (a *API) bootstrapAllowed(r *http.Request) bool {
	if a.bootstrapToken == "" {
		return false
	}
	got := r.Header.Get("X-Bootstrap-Token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(a.bootstrapToken)) == 1
}

func (a *API) securityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/mfa/enroll", a.handleMFAEnroll)
	mux.HandleFunc("POST /api/v1/auth/mfa/confirm", a.handleMFAConfirm)
	mux.HandleFunc("POST /api/v1/auth/password", a.handleChangePassword)
	mux.HandleFunc("POST /api/v1/auth/reset-password", a.handleCompleteReset)
	mux.HandleFunc("POST /api/v1/users/{id}/reset-password", a.handleIssueReset)
	mux.HandleFunc("POST /api/v1/users/{id}/mfa/reset", a.handleResetMFA)
	mux.HandleFunc("POST /api/v1/users/{id}/active", a.handleSetActive)
	mux.HandleFunc("POST /api/v1/users/{id}/roles", a.handleSetRoles)
}

// ---- transport security ----

// isHTTPS is true for direct TLS or when our own reverse proxy (a private or
// loopback peer) says the client used HTTPS.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate()) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// contentSecurityPolicy: scripts only from our own origin (no 'unsafe-inline'
// for scripts, which is what makes a stolen-token XSS hard). Styles keep
// 'unsafe-inline' because MUI/emotion injects <style> tags at runtime; the UI
// also pulls web fonts from Google Fonts.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com data:; img-src 'self' data:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func (a *API) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin is the CSRF check for cookie-authenticated, state-changing
// requests: the browser must say the request came from this very origin (or an
// explicitly allowed one). SameSite=Strict on the cookie is the first layer,
// this is the second.
func (a *API) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	if a.allowedOrigins[origin] {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

// withSession lets the browser UI authenticate with an HttpOnly cookie instead
// of a token kept in JavaScript-readable storage. Non-browser clients keep
// using `Authorization: Bearer` and are not subject to the Origin check (they
// are not exposed to CSRF: a browser never attaches that header by itself).
func (a *API) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
				if isMutating(r.Method) && !a.sameOrigin(r) {
					a.audit(r, "csrf.blocked", "failure", "origin", r.Header.Get("Origin"))
					writeJSON(w, http.StatusForbidden, &AppError{Code: "forbidden", Message: "cross-origin request refused"})
					return
				}
				r.Header.Set("Authorization", "Bearer "+c.Value)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// respondAuth answers login/bootstrap. A client that sends
// `X-Session-Mode: cookie` (the web UI) gets the token ONLY as an HttpOnly
// cookie and an empty token field; everyone else keeps the old behaviour.
func (a *API) respondAuth(w http.ResponseWriter, r *http.Request, status int, v AuthResponse) {
	if strings.EqualFold(r.Header.Get("X-Session-Mode"), "cookie") {
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookie, Value: v.Token, Path: "/api", HttpOnly: true,
			Secure: isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds()),
		})
		v.Token = ""
	}
	writeJSON(w, status, v)
}

func (a *API) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/api", HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

func metricReason(r string) string {
	switch r {
	case "unknown_or_inactive", "locked", "bad_password", "bad_password_lockout_started", "bad_totp",
		"bad_totp_lockout_started", "mfa_code_missing", "totp_replay", "changed_during_login", "oversized_password":
		return r
	}
	return "other"
}

// ---- readiness ----

func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := a.service.Ready(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

// ---- security handlers ----

func (a *API) handleMFAEnroll(w http.ResponseWriter, r *http.Request) {
	v, err := a.service.EnrollMFA(a.token(r))
	if err != nil {
		a.writeError(w, err)
		return
	}
	a.audit(r, "mfa.enroll_started", "success")
	writeJSON(w, http.StatusOK, v)
}

func (a *API) handleMFAConfirm(w http.ResponseWriter, r *http.Request) {
	var req MFAConfirmRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	v, err := a.service.ConfirmMFA(a.token(r), req)
	if err != nil {
		a.audit(r, "mfa.enroll_confirmed", "failure")
		a.writeError(w, err)
		return
	}
	a.audit(r, "mfa.enroll_confirmed", "success")
	writeJSON(w, http.StatusOK, v)
}

func (a *API) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	if err := a.service.ChangePassword(a.token(r), req); err != nil {
		a.audit(r, "password.changed", "failure")
		a.writeError(w, err)
		return
	}
	a.audit(r, "password.changed", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleCompleteReset(w http.ResponseWriter, r *http.Request) {
	var req CompleteResetRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	if err := a.service.CompleteReset(req); err != nil {
		reason := ""
		if ae, ok := err.(*AppError); ok {
			reason = ae.Reason
		}
		a.audit(r, "password.reset_completed", "failure", "reason", reason)
		a.writeError(w, err)
		return
	}
	a.audit(r, "password.reset_completed", "success")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *API) handleIssueReset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	v, err := a.service.IssueResetToken(a.token(r), id)
	if err != nil {
		a.audit(r, "password.reset_issued", "failure", "target_user_id", id)
		a.writeError(w, err)
		return
	}
	a.audit(r, "password.reset_issued", "success", "target_user_id", id) // the token itself is never logged
	writeJSON(w, http.StatusOK, v)
}

func (a *API) handleResetMFA(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	v, err := a.service.ResetMFA(a.token(r), id)
	if err != nil {
		a.audit(r, "mfa.reset", "failure", "target_user_id", id)
		a.writeError(w, err)
		return
	}
	a.audit(r, "mfa.reset", "success", "target_user_id", id)
	writeJSON(w, http.StatusOK, v)
}

func (a *API) handleSetActive(w http.ResponseWriter, r *http.Request) {
	var req SetActiveRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	id := r.PathValue("id")
	v, err := a.service.SetUserActive(a.token(r), id, req.Active)
	if err != nil {
		a.audit(r, "user.set_active", "failure", "target_user_id", id, "active", req.Active)
		a.writeError(w, err)
		return
	}
	a.audit(r, "user.set_active", "success", "target_user_id", id, "active", req.Active)
	writeJSON(w, http.StatusOK, v)
}

func (a *API) handleSetRoles(w http.ResponseWriter, r *http.Request) {
	var req SetRolesRequest
	if err := decodeJSON(r, &req); err != nil {
		a.writeError(w, badRequest(err.Error()))
		return
	}
	id := r.PathValue("id")
	v, err := a.service.SetUserRoles(a.token(r), id, req)
	if err != nil {
		a.audit(r, "user.set_roles", "failure", "target_user_id", id)
		a.writeError(w, err)
		return
	}
	a.audit(r, "user.set_roles", "success", "target_user_id", id, "roles", v.Roles)
	writeJSON(w, http.StatusOK, v)
}
