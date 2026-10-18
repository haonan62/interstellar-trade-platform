# Phase 4 — Security hardening

Marks: **VERIFIED** = I ran it (test or live container). **ASSUMED** = not run.

## Decisions

| Area | Decision | Alternatives rejected |
|---|---|---|
| Bootstrap | `POST /auth/bootstrap` needs the header `X-Bootstrap-Token` equal to `BOOTSTRAP_TOKEN` (constant-time compare, checked before the body is parsed). No token configured and system empty → server generates one and logs it once. Never configured and already bootstrapped → irrelevant. | Open endpoint (status quo); IP allow-list (breaks behind a proxy). |
| Login throttling | Per **account**: 5 consecutive failures → 15 min lock, counter stored in the user document so it holds across replicas/restarts. Locked/unknown/bad password all return the same 401 `invalid credentials`; the cause goes only to the audit log (`AppError.Reason`, never serialised). nginx adds a per-IP limit. | Per-IP only (bypassed by botnets); in-memory counters (lost on restart, not shared). |
| MFA | RFC 6238 TOTP (SHA-1, 6 digits, ±1 step), standard library only. Enrolment is two-step (pending secret, confirmed by a valid code). Each accepted step is stored; a step can never be reused (replay guard, re-checked inside the write transaction). `REQUIRE_ADMIN_MFA=true` gates every API call of an admin without MFA except `/auth/me` and the enrolment endpoints. A wrong code counts as a failed login. | WebAuthn (better, needs a library + frontend work: future); SMS (weak). |
| Password reset | No e-mail channel exists, so reset is admin-initiated: a one-time token `<userID>.<32 random bytes>`, only its sha256 stored, 1 h expiry, single use. Completing it clears lockout and revokes every session. Self-service change needs the current password and revokes other sessions. | Link by e-mail (no mail infrastructure); security questions. |
| Deactivation / roles | `POST /users/{id}/active`, `/roles`. Deactivation revokes all sessions. Colony admins act only inside their colony; only super_admin touches a super_admin; nobody modifies themself through these endpoints, so a super_admin always remains. Both are written to the colony ledger. | Deleting users (breaks ledger/trade references). |
| Audit | Structured JSON events (`audit:true`, event, outcome, request_id, trace_id, user_id, ip) for logins, bootstrap, MFA, resets, role/active changes, CSRF blocks, plus one `http.mutation` event per state-changing request. Secrets (passwords, reset tokens, session tokens) are never logged. | A database audit table (see risks). |
| CORS | Fail closed: empty allow-list = no CORS headers. | Allow-all on empty (status quo). |
| Visibility | `GET /users`: super_admin and the user's own colony get the full view; other colonies' users only expose id, username, display name, colony, active (needed to pick a counterparty). Ledger/accounts were already colony-scoped. | Hiding foreign users entirely (breaks cross-colony offers in the UI). |
| Token storage | The UI sends `X-Session-Mode: cookie`; the server then sets an `HttpOnly; SameSite=Strict; Path=/api` (+`Secure` behind HTTPS) cookie and returns an empty `token`. Cookie-authenticated state-changing requests must carry an `Origin` equal to `Host` (or an allowed origin). Bearer clients (`demo.sh`, scripts) are unchanged. `localStorage` is no longer used (and old tokens are wiped). | Keeping localStorage + stricter CSP only. |
| CSP / headers | Set by the app on every response: `script-src 'self'` (no inline scripts; the Vite build has none), `frame-ancestors 'none'`, `object-src 'none'`, `base-uri 'none'`, nosniff, `Referrer-Policy: no-referrer`, COOP/CORP, `Cache-Control: no-store` on `/api/`, HSTS when HTTPS is certain. `style-src` keeps `'unsafe-inline'` (MUI/emotion inject style tags) and Google Fonts hosts. | Nonce-based styles (needs emotion nonce plumbing). |
| Container | Non-root `65532`, `cap_drop ALL`, read-only root, `no-new-privileges`, base images pinned by digest, Dependabot for digests/modules/npm/actions. | |

## Data-model / wire changes (all additive, no migration needed)
- `User` JSON gained optional fields (`failed_logins`, `locked_until`, `mfa_*`, `reset_*`, `password_changed_at`); they live in the existing `users.doc` jsonb, old documents load unchanged.
- New endpoints: `/auth/mfa/enroll|confirm`, `/auth/password`, `/auth/reset-password`, `/users/{id}/reset-password|mfa/reset|active|roles`, `/readyz`.
- New optional request fields/headers: `totp_code` on login, `X-Bootstrap-Token`, `X-Session-Mode`. New error codes `mfa_required`, `mfa_enrollment_required`.
- **Behaviour changes** to existing API: bootstrap now needs the token (breaks naive fresh-deploy scripts); CORS no longer allows all origins when unset; `GET /users` shrinks foreign users' details. `scripts/demo.sh` bootstraps without a token, so it needs `X-Bootstrap-Token` now (see Known gaps).

## Verified
- Go tests (`phase4_test.go`, 20 tests), `-race`, **file store and a real PostgreSQL 17**: RFC 6238 vector, lockout and recovery, same public answer for locked vs unknown user, MFA enrol/replay/lockout, admin-MFA gate, reset (single use, expiry, forged, clears lockout, revokes sessions), password change, deactivate/roles authorisation matrix, role change in ledger, MFA reset, `GET /users` scoping, bootstrap gate (none/wrong/right/second/unconfigured), CORS fail-closed, security headers (no HSTS from a forged proto header), cookie login + CSRF Origin matrix, request-id/traceparent sanitising, metrics label cardinality, audit lines carry request/user ids and contain no secrets.
- Live container (smoke script `scripts/smoke_security.sh`): bootstrap 403 without token / 201 with; cookie is `HttpOnly; SameSite=Strict`, token field empty; cookie POST without Origin 403, same Origin 201; CORS preflight from a foreign origin gets no allow-header; 5 bad logins then the right password still 401 (locked); container user `65532`.
- `govulncheck`: found `golang.org/x/text` 0.37.0 (reachable through pgx); upgraded to 0.41.0; no reachable findings remain. `npm audit --omit=dev`: 0.

## Known gaps / risks (not done)
- **TOTP secrets, user and colony private keys are plaintext in the state file/database** (needs the Phase 2 KMS interface). A database dump exposes them.
- Audit is a log stream, not a tamper-evident store; retention depends on your log pipeline.
- Lockout is a denial-of-service lever for someone who knows a username (mitigation: admin reset, nginx IP limit). No CAPTCHA or progressive delay.
- No recovery codes for MFA (admin resets instead); no WebAuthn.
- Session fixation window is small but sessions are 24 h fixed, no sliding expiry or "list/revoke my sessions" endpoint.
- Sessions of a user whose role is demoted are not revoked (roles are read live per request, so effect is immediate).
- X-Forwarded-Proto/X-Real-IP are trusted only from loopback/private peers; if the app is ever exposed to a private network with untrusted peers, revisit.
- `scripts/demo.sh` now needs `BOOTSTRAP_TOKEN` (updated and **run against the container image: trade reached `completed`**, VERIFIED).
- Trivy image scan is configured in CI but I have **not** run it locally (ASSUMED).


