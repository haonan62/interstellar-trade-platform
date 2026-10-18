# Interstellar Trade Platform

A full-stack proof-of-concept for delayed inter-colony digital trade: colonies hold accounts and ledgers, and trade with each other asynchronously through signed envelopes (offer → acceptance → settlement) carried in relay bundles. There is no global real-time consensus.

> **Status:** a hardened POC, not a finished production system. Read [docs/production-readiness.md](docs/production-readiness.md) for what is done and the remaining risks (notably: all colonies share one process/database and private keys are stored in plaintext).

## What it includes

- Go backend (REST), two storage backends: a JSON file (development) and PostgreSQL (deployment)
- Session auth (HttpOnly cookie for the UI, bearer token for scripts), roles `super_admin`, `colony_admin`, `trader`, `relay_operator`
- Account lockout, optional TOTP MFA (required for admins on request), password reset, user deactivation, audit log
- Colony-local balances and hash-chained ledgers, signed envelopes, async relay export/import
- Trade state machine with expiry, cancel and refund; mint caps and optional dual control
- React + Material UI frontend
- Prometheus metrics, JSON logs, health/readiness probes

## Project layout

```text
backend/            Go service (stdlib + pgx + x/crypto); migrations/ = PostgreSQL schema
frontend/           React + Vite app
deploy/             nginx.conf, nginx-http-bootstrap.conf, RUNBOOK.md
docs/               design docs per phase, production-readiness checklist
scripts/            demo.sh, pg_backup.sh, pg_restore_drill.sh, nginx_check.sh, smoke_security.sh
Dockerfile          multi-stage build (frontend, backend, FROM scratch runtime)
compose.yml         API container (JSON-file store)
compose.postgres.yml  overlay: adds PostgreSQL and points the API at it
.env.example        configuration template
```

## Local development

### Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.25+ | matches `backend/go.mod` |
| Node.js | 20+ (22 recommended) | Vite 7 does not run on older Node |
| Docker | optional | only for PostgreSQL tests, image builds, `nginx -t` |

### 1. Run the backend

```bash
cd backend
BOOTSTRAP_TOKEN=dev-token go run . -addr :8080 -data ./data/state.json
```

- State is stored in `backend/data/state.json` (git-ignored). Delete it to start from scratch.
- `BOOTSTRAP_TOKEN` is the one-time secret for creating the first admin. If you omit it, the server generates one and prints it in the start-up log (`bootstrap_token`).
- Logs are JSON; set `LOG_LEVEL=debug` for more.

### 2. Run the frontend

```bash
cd frontend
npm install
npm run dev          # http://localhost:5173
```

The Vite dev server proxies `/api` to `http://localhost:8080`, so the browser stays on one origin (no CORS setup needed; CORS is closed by default). On the first visit, the UI asks for the bootstrap token and creates the first administrator.

### 3. Try the whole flow without the UI

```bash
BOOTSTRAP_TOKEN=dev-token ./scripts/demo.sh     # needs a fresh state file and `python` on PATH
```

It bootstraps an admin, creates Earth and Mars, trusts them mutually, creates users, mints funds and runs a full trade through export/import.

### Local PostgreSQL (optional)

```bash
docker run -d --name itp-pg -e POSTGRES_PASSWORD=itp -e POSTGRES_DB=itp -p 5432:5432 postgres:17-alpine
cd backend
BOOTSTRAP_TOKEN=dev-token go run . -addr :8080 \
  -database-url 'postgres://postgres:itp@localhost:5432/itp?sslmode=disable'
```

Migrations are applied automatically at start-up. Move an existing JSON state into an empty database once with `--migrate-state-from ./data/state.json` (same flags plus `-database-url`; the process exits when done).

### Tests and checks

```bash
cd backend
gofmt -l .                      # must print nothing
go vet ./...
go test -race -count=1 ./...    # file store

# same suite against a real PostgreSQL (extra PG-only tests run too)
TEST_DATABASE_URL='postgres://postgres:itp@localhost:5432/itp?sslmode=disable' go test -race -count=1 ./...
```

Other checks: `./scripts/nginx_check.sh` (validates the nginx configs with `nginx -t` in Docker) and `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`. CI (`.github/workflows/ci.yml`) runs all of these plus a frontend build, `npm audit` and an image scan.

## Configuration

Environment variables (or the equivalent flags; see `go run . -h`). Copy `.env.example` to `.env` for Docker Compose.

| Variable | Default | Purpose |
|---|---|---|
| `BOOTSTRAP_TOKEN` | generated and logged once | one-time secret for creating the first admin (`X-Bootstrap-Token` header) |
| `CORS_ORIGINS` | empty = none allowed | comma-separated origins allowed cross-origin. Leave empty for the bundled UI (same origin) |
| `REQUIRE_ADMIN_MFA` | `false` | `true`: admins must enrol TOTP before using the API |
| `DATABASE_URL` | unset = JSON file store | PostgreSQL connection URL |
| `METRICS_ADDR` | disabled | e.g. `:9090` serves `/metrics` on a separate listener; never expose it publicly |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `POSTGRES_PASSWORD` | required with `compose.postgres.yml` | database password |

Flags without an environment variable: `-addr` (`:8080`), `-data` (state file path), `-static` (directory of built frontend files to serve).

## Build

```bash
docker build -t interstellar-trade:latest .
# or: docker compose build
```

The multi-stage `Dockerfile` builds the React app (Node 22), compiles a static Go binary (Go 1.25), and produces a `FROM scratch` image that contains only the binary, CA certificates and the frontend assets. It runs as non-root user `65532`, and base images are pinned by digest (Dependabot proposes updates). `frontend/package-lock.json` must be committed because the build uses `npm ci`.

The Go server serves both the API and the built UI on port 8080.

## Deploy

Target: one Linux VM, Docker Compose, nginx on the host for TLS. The step-by-step procedure (VM preparation, certificates, first login, backups, monitoring, user administration, troubleshooting) is in **[deploy/RUNBOOK.md](deploy/RUNBOOK.md)**. In short:

```bash
git clone <repo> && cd interstellar-trade-platform
cp .env.example .env
$EDITOR .env          # BOOTSTRAP_TOKEN (openssl rand -base64 24), POSTGRES_PASSWORD, ...
```

**With PostgreSQL (recommended):**

```bash
docker compose -f compose.yml -f compose.postgres.yml up -d --build
```

**With the JSON file store (simple, single instance, global write lock):**

```bash
docker compose up -d --build
```

Then:

1. Check health: `docker compose ps` (the container reports `healthy`) and `curl http://127.0.0.1:8080/api/v1/readyz`.
2. Put nginx in front: install `deploy/nginx-http-bootstrap.conf`, obtain a certificate with certbot, then switch to `deploy/nginx.conf` (RUNBOOK sections 3 and 4). The API port is bound to `127.0.0.1` only and must not be opened to the internet.
3. Open the site, create the first admin with the bootstrap token, enrol MFA, then set `REQUIRE_ADMIN_MFA=true` in `.env` and run `docker compose up -d` again.
4. Schedule backups (PostgreSQL: `scripts/pg_backup.sh`, verify with `scripts/pg_restore_drill.sh`; see RUNBOOK and `docs/phase3-design.md`).

Updating: `git pull && docker compose [-f compose.yml -f compose.postgres.yml] up -d --build`. Data lives in named volumes (`pg-data`, `trade-data`) and survives rebuilds. `docker compose down -v` deletes them.

## API surface

All paths are under `/api/v1`. Authenticated calls use the session cookie (UI) or `Authorization: Bearer <token>`.

```text
Health        GET  /healthz   GET /readyz
Bootstrap     GET  /bootstrap                      POST /auth/bootstrap   (X-Bootstrap-Token)
Auth          POST /auth/login  /auth/logout       GET  /auth/me
              POST /auth/password                  POST /auth/reset-password
              POST /auth/mfa/enroll  /auth/mfa/confirm
Overview      GET  /dashboard  /colonies  /accounts  /trades  /trades/{id}  /ledger?colony_id=&limit=&before_seq=
Colonies      POST /colonies   /colonies/{id}/trust   /colonies/{id}/policy
Users         GET  /users      POST /users
              POST /users/{id}/active  /users/{id}/roles  /users/{id}/reset-password  /users/{id}/mfa/reset
Money         POST /accounts/mint
Trades        POST /trades/offers  /trades/{id}/accept  /trades/{id}/cancel  /trades/{id}/force-refund
Approvals     GET  /approvals      POST /approvals/{id}/approve  /approvals/{id}/reject
Relay         POST /relay/export   /relay/import
```

## Documentation

- [deploy/RUNBOOK.md](deploy/RUNBOOK.md): deployment and operations
- [docs/phase1-design.md](docs/phase1-design.md): trade state machine, expiry/refund, ledger conservation
- [docs/phase3-design.md](docs/phase3-design.md): PostgreSQL, HA, backups and restore drill
- [docs/phase4-design.md](docs/phase4-design.md): security hardening
- [docs/phase5-design.md](docs/phase5-design.md): deployment and observability
- [docs/production-readiness.md](docs/production-readiness.md): checklist and remaining risks

## Notes

- This is a POC: colonies are simulated inside one process and one database, the server signs on behalf of users, and private keys and MFA secrets are stored unencrypted. Do not use real value.
- The system is intentionally asynchronous; light-delay latency is a design assumption.

