# Phase 5 — Deploy and operations

## What changed

| Item | Before | Now | Status |
|---|---|---|---|
| Healthcheck | `wget` + `/bin/sh` in a `FROM scratch` image: could never pass | The binary probes itself (`--healthcheck` → `GET 127.0.0.1:<port>/api/v1/readyz`); Dockerfile `HEALTHCHECK` + compose `CMD`. `/readyz` pings the store (PostgreSQL) so a dead database shows as unhealthy; `/healthz` stays a pure liveness probe. | VERIFIED: container reached `healthy` |
| nginx | `upstream` inside `server {}` (invalid); API limit 30 r/min throttled the SPA; the auth limit also covered `/auth/me` and `/auth/logout`; nested `add_header` dropped the security headers; `Host $host` loses the port the CSRF check compares | `upstream` at http level; API zone 10 r/s burst 100; the strict 10 r/min zone only on exact `login`, `bootstrap`, `reset-password`; app sets security headers itself (nginx adds only HSTS); `Host $http_host`; `X-Request-Id` / `traceparent` forwarded; relay import allows 8 MiB; `/metrics` not proxied | VERIFIED: `nginx -t` passes in the real nginx image (`scripts/nginx_check.sh`) for both files. Not verified: behaviour under load / real TLS |
| TLS bootstrap order | Config referenced certificates that did not exist yet, nginx could not start before certbot | HTTP-only `nginx-http-bootstrap.conf` first, `certbot certonly --webroot` with a reload hook, then the full config | `nginx -t` VERIFIED; the certbot steps are ASSUMED (no domain here) |
| `.env.example` | referenced by the runbook, missing | created; `.env` and `backups/` git-ignored | VERIFIED (file exists, compose reads it) |
| Lockfile | `frontend/package-lock.json` git-ignored although `npm ci` needs it | un-ignored (must be committed: **`git add frontend/package-lock.json`**); CI has a frontend `npm ci && build` job | Docker build with `npm ci` VERIFIED |
| Stale comments | entrypoint-wrapper comment, wrong `--cors` default | removed; configuration is environment-driven | |
| Structured logging | `log.Printf` text | JSON via `log/slog` (`LOG_LEVEL`), request/audit lines carry `request_id`, `trace_id`, `span_id`, `user_id`, `ip` | VERIFIED in tests and in the container |
| Metrics | none | Prometheus text at `METRICS_ADDR` (separate internal listener, off by default, `:9090` in compose, not published): request count/latency histogram by method+route pattern+status (unmatched paths share one label), in-flight, login failures by reason, audit events, goroutines, memory. Hand-written exposition (~100 lines) instead of `client_golang`, which would add ~10 modules for features not used. | VERIFIED in tests and by scraping the running container |
| Tracing | none | **W3C trace-context correlation only**: a valid `traceparent` trace id is adopted (else one is generated), logged, and returned in `Traceresponse`. No spans are exported. A real OpenTelemetry SDK/OTLP exporter is **not implemented** (heavy dependency tree and needs a collector). | VERIFIED (propagation test); export NOT DONE |
| Runbook | outdated | bootstrap, MFA, user admin, monitoring, probes, volume ownership, new troubleshooting rows; backup/restore drill from Phase 3b (`scripts/pg_backup.sh`, `pg_restore_drill.sh`) | |

## Known gaps
- No Prometheus alert rules, dashboards or log shipping are provided; no alerting means nobody is paged.
- The deployment is still a single VM with one app container; no HA proxy tier, no automated failover (Phase 3 describes the PostgreSQL side only).
- `REQUIRE_ADMIN_MFA` defaults to `false` so a first deployment can bootstrap; enabling it is a manual runbook step.
- Rate limits are per client IP at nginx; behind another proxy/CDN, `$remote_addr` would be the proxy (configure `real_ip`).
- The restore drill is a logical dump on a tiny dataset (see Phase 3 doc); no PITR.

