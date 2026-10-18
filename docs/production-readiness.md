# Production-readiness checklist and remaining risks

Status key: **DONE** (built and verified by running it), **PARTIAL**, **NOT DONE**.
Verification scope: unit/integration tests with `-race` on the file store and on PostgreSQL 17, a built container image run locally, `nginx -t`, `govulncheck`, `npm audit`. Nothing here has run on a real internet-facing host, with a real domain/TLS certificate, or under load.

## Checklist

| Area | Item | Status |
|---|---|---|
| Phase 0 | Regression tests for the known issues, CI (build, vet, race, lint), go.mod/Dockerfile match | DONE |
| Phase 1 | Atomic updates, trade state machine, expiry/cancel/refund, checked arithmetic, mint caps, dual control | DONE (see `phase1-design.md`) |
| Phase 1 | Asset ownership / delivery leg | Documented non-goal (see phase 1 doc) |
| Phase 2 | Independent colony nodes, client-side user signing, KMS/HSM, key rotation/revocation, canonical serialisation + domain separation, hash-linked signed sequence numbers, signed checkpoints, bundle-ID validation | **NOT DONE**. All colonies still live in one process and one database; the server still signs for users; private keys are plaintext. "Inter-colony" trade is simulated. No trust-model ADR was written because the design was not implemented |
| Phase 3 | PostgreSQL with transactional ledger, idempotency by constraint, outbox table, per-colony locking, stateless replicas, sessions in DB, argon2id, pagination, timeouts, body limits, graceful shutdown, `state.json` migration | DONE |
| Phase 3 | Encrypted backup + restore drill | DONE for logical dumps (see `phase3-design.md`) |
| Phase 3 | PITR/WAL archiving, off-host copy automation, replica, failover drill, measured RTO/RPO | **NOT DONE** (targets only) |
| Phase 4 | Bootstrap token, account lockout, TOTP MFA + admin requirement, password reset/change, deactivation, role change, audit logging, CORS fail-closed, user-list scoping, cookie sessions + CSRF, CSP/security headers, non-root, pinned digests, vulnerability scanning | DONE (see `phase4-design.md`) |
| Phase 4 | WebAuthn, MFA recovery codes, tamper-evident audit store, secrets encryption at rest | **NOT DONE** |
| Phase 5 | Healthcheck, nginx (validated), rate limits, `.env.example`, lockfile, structured logs, metrics, runbook | DONE (see `phase5-design.md`) |
| Phase 5 | Distributed tracing export (OTLP), alert rules, dashboards | **NOT DONE** (trace-id correlation only) |

## ADR: database (PostgreSQL)

- **Decision**: PostgreSQL via `pgx/v5`, scoped transactions (rows a handler declares, colonies locked `FOR UPDATE` in sorted order), state kept in the same `AppState` shape the handlers always used, diff-persisted. The JSON file store remains for development/tests.
- **Why**: transactional ledger, uniqueness/`CHECK` constraints enforce idempotency and non-negative balances in the database rather than in code, independent colony pairs run in parallel, replicas become stateless.
- **Alternatives**: SQLite (single writer, no HA); a repository interface rewrite of every handler (more invasive, same outcome); event sourcing (more moving parts than needed).
- **Consequences**: raw payload columns are `json`, not `jsonb`, because key re-ordering breaks signatures and ledger hashes; additive user fields live in `users.doc` and need no migration; the scoped-load layer is the main complexity to keep in sync with handlers.
- **Not decided here**: the Phase 2 trust-model ADR (nothing to record until it is designed and implemented).

## Remaining risks, highest first

1. **Plaintext private keys and MFA secrets in the database/state file**, and the server signs on behalf of users and colonies: a database or server compromise lets an attacker forge any trade. Signatures give no non-repudiation. (Phase 2 / KMS.)
2. **Single trust domain**: one process, one database, one operator. There is no real independence between colonies.
3. **Recovery is untested at scale**: logical backups only, RPO = backup interval, no PITR, no off-host copy automation, no measured RTO.
4. **No alerting**: metrics and audit logs exist, but nothing watches them.
5. **Login lockout can be abused for denial of service** against a known username; mitigation is admin reset plus nginx IP limits.
6. **Admin MFA is opt-in** (`REQUIRE_ADMIN_MFA`); a fresh deployment is single-factor until the runbook step is done.
7. **Behaviour not proven under load**: lock contention, connection pool sizing (max 20), nginx limits, and Argon2 memory under a login flood were only tested functionally.
8. **Image/dependency scanning in CI (trivy, govulncheck, npm audit) is configured; only govulncheck and npm audit were run locally.** Four unreachable Go-module advisories remain reported by govulncheck (not called by this code).
9. Unit of work still in the working tree: **nothing is committed yet**; the changes of Phases 0–5 are uncommitted (and `frontend/package-lock.json` must be added).

