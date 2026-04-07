# Interstellar Trade Platform

A full-stack proof-of-concept for delayed inter-colony digital trade.

## What it includes

- Go backend with REST APIs
- File-backed persistent state
- Bearer-token authentication
- Role-based authorization for `super_admin`, `colony_admin`, `trader`, `relay_operator`
- Colony-local account balances and ledgers
- Signed trade offers, acceptances, and settlement envelopes
- Async relay export/import flow between colonies
- React + Material UI frontend for common workflows

## Project layout

```text
interstellar-trade-platform/
  README.md
  backend/
    go.mod
    *.go
  frontend/
    package.json
    vite.config.js
    index.html
    src/
      main.jsx
      api.js
      App.jsx
  scripts/
    demo.sh
```

## Backend API surface

```text
GET    /api/v1/healthz
GET    /api/v1/bootstrap
POST   /api/v1/auth/bootstrap
POST   /api/v1/auth/login
POST   /api/v1/auth/logout
GET    /api/v1/auth/me
GET    /api/v1/dashboard
GET    /api/v1/colonies
POST   /api/v1/colonies
POST   /api/v1/colonies/{id}/trust
GET    /api/v1/users
POST   /api/v1/users
GET    /api/v1/accounts
POST   /api/v1/accounts/mint
GET    /api/v1/trades
GET    /api/v1/trades/{id}
POST   /api/v1/trades/offers
POST   /api/v1/trades/{id}/accept
POST   /api/v1/relay/export
POST   /api/v1/relay/import
GET    /api/v1/ledger?colony_id=<id>&limit=200
```

## Backend run

```bash
cd backend
go run . -addr :8080 -data ./data/state.json
```

## Frontend run

```bash
cd frontend
npm install
npm run dev
```

The Vite dev server proxies `/api` to `http://localhost:8080`.

## Frontend workflows supported

- bootstrap first admin
- login and logout
- create colonies
- establish peer trust
- create users and assign roles
- mint colony-local funds
- create trade offers
- accept imported offers
- export relay bundles
- import relay bundles
- inspect colony ledgers and obligations

## Notes

- This is still a POC. Password hashing is implemented directly in the service to avoid external Go dependencies.
- Private keys are stored in the backend state file for simplicity.
- The system is intentionally asynchronous. There is no global real-time consensus.
- The frontend is intentionally compact and favors functionality over polish.
