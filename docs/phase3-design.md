# Phase 3 — Persistence, HA and concurrency

## 3a — DONE and VERIFIED (no PostgreSQL needed)

| Item | Change |
|---|---|
| Hashing outside the lock | `Login` reads credentials under RLock, verifies **without any lock**, then takes the write lock only for the session insert (re-validating active flag and hash). `CreateUser`/`Bootstrap` authorise first, hash, then lock. Hash concurrency is bounded (`hashSem`, min 2, NumCPU) so a login flood cannot exhaust RAM. Unknown users burn an equal verification (timing). |
| argon2id + migration | `golang.org/x/crypto/argon2`, OWASP minimum profile (m=19 MiB, t=2, p=1), PHC string in `PasswordHash`. Legacy PBKDF2 users verify with the old routine and are upgraded on next successful login. Tampered PHC parameters (memory bombs) are refused. |
| Session pruning | Expired sessions are pruned on every login; at most 10 live sessions per user (oldest evicted). |
| Server hardening | `ReadHeaderTimeout 5s`, `ReadTimeout 15s`, `WriteTimeout 30s`, `IdleTimeout 60s`, 16 KiB headers; body limit 1 MiB (8 MiB for relay import): 413 when the length is declared, truncated read otherwise. |
| Graceful shutdown | SIGTERM/SIGINT stops accepting, drains in-flight requests (20 s), stops the sweeper. |
| Ledger pagination | `limit` defaults to 200 (max 1000) and only the returned page is copied; new optional `before_seq` cursor. The UI already sends `limit=200`. |

Dependency justification: `golang.org/x/crypto` is the Go project's own module and the only maintained argon2id implementation outside cgo; there is no stdlib equivalent. Version 0.52.0 is the first without known CVEs (the older ones were in `x/crypto/ssh`, which we do not import). It raises the minimum Go to 1.25, so `go.mod`, the Dockerfile and CI moved to 1.25.

Still true after 3a: every write copies and rewrites the whole state file (the file store); the global mutex still serialises all writes.

## 3b — PostgreSQL: IMPLEMENTED (see "What was built" and "Known gaps")

### What was built
- `migrations/001_init.sql` (embedded, applied under an advisory lock at start-up): typed money columns with `CHECK (balance >= 0)`; idempotency by constraint (`processed_envelopes` PK, `ledger_entries` PK `(colony_id, seq)`, `bundles` PK with `ON CONFLICT DO NOTHING` so an imported bundle can never overwrite a stored one); unique lower-case usernames and colony names; relay `outbox` table written in the same transaction as the change that produced the message; indexed trade queries and a partial index for the expiry sweep.
- **Scoped transactions**: every handler declares a `Scope` (tokens, users, trades, colonies, envelope ids...). The store locks only the colonies in scope (`FOR UPDATE`, sorted by id, so no deadlocks), loads the minimal rows into the same `AppState` the handlers always used, runs the handler, and persists only the diff. Different colony pairs run in parallel; the global mutex is gone on this backend.
- **Stateless replicas**: sessions live in the `sessions` table (pruned by SQL, capped per user); nothing else is kept in process. The expiry sweeper is idempotent, so every replica may run it.
- **Pagination in SQL**: ledger (`LIMIT` + `before_seq`), trades (newest N visible to the actor), users, accounts, counts via `count(*)`.
- **Migration**: `--database-url ... --migrate-state-from state.json` verifies hash chains, sequence continuity and money conservation first, refuses a non-empty database, and imports in one SERIALIZABLE transaction.
- `compose.postgres.yml` adds the database; CI runs the whole suite on both backends.

### Bug found on the way
`json` vs `jsonb`: jsonb re-orders keys and re-spaces text, which silently breaks signatures and ledger hashes that cover raw payload bytes. Columns that carry raw payloads (ledger, outbox, bundles) are therefore `json`. Separately, the **file** store wrote raw payloads re-indented (`MarshalIndent`), so ledger hashes in `state.json` no longer verified after any restart; payloads are now compacted on load (lossless) and a regression test covers it.

### Known gaps (not done)
- Dashboard/list endpoints still return everything up to fixed caps (users 1000, accounts 2000, trades 500); they do not yet have cursor pagination in the REST API.
- Secrets (user and colony private keys) are still stored in plaintext, now in Postgres JSON. That is Phase 2/4 work.
- No read replicas, no PITR (needs a WAL-archiving server), no failover drill. Backups are logical only, see "Backups and restore drill".
- Connection URL uses `sslmode=disable` in the compose example (private network only).
- The old file store remains for development; it still has the global lock.

### Original rationale (kept for reference)

Why it is not a drop-in: handlers read `state.Users[...]`, `state.Trades[...]`, iterate all users to find a username, and hold each colony's ledger and processed-envelope set in memory. A real database store needs those access patterns replaced, not just a new `saveJSONAtomic`.

Proposed approach
1. **Schema** (`db/migrations/001_init.sql`): `colonies`, `users` (unique `lower(username)`), `sessions` (indexed `expires_at`), `accounts(colony_id,user_id,balance CHECK >= 0)`, `trades`, `ledger_entries(colony_id, seq, ..., UNIQUE(colony_id, seq))`, `envelopes_outbox` (the relay outbox), `processed_envelopes(colony_id, envelope_id) PRIMARY KEY` (idempotency by constraint), `bundles(id PRIMARY KEY)` (no overwrite), `pending_actions`.
2. **Concurrency**: a transaction locks only the colonies it touches (`SELECT ... FOR UPDATE` in sorted ID order to avoid deadlocks). Different colony pairs proceed in parallel; the global mutex goes away.
3. **Service seam**: replace `StoreUpdate(fn(*AppState))` by a repository interface (`Tx.User(id)`, `Tx.Trade(id)`, `Tx.Account(colony,user)`, `Tx.AppendLedger(...)`, `Tx.MarkProcessed(...)`), with an in-memory implementation kept for tests and a `pgx` implementation. Handlers are ported one at a time under the existing tests.
4. **Stateless replicas**: sessions in the `sessions` table (already pruned and capped by 3a); no in-process state besides the sweeper, which takes `pg_try_advisory_lock` so only one replica runs it.
5. **Migration**: `cmd migrate-state --from state.json --to $DATABASE_URL` in one transaction; verifies per-colony ledger hash chains and that `sum(balances)+escrow == minted_total` before commit; idempotent via a `schema_migrations` row.
6. **Outbox**: the relay outbox is a table written in the same transaction as the state change that produced the message (no lost or phantom messages).
7. Driver: `github.com/jackc/pgx/v5` (maintained, context-aware, no cgo); tests against a real Postgres in Docker (skipped without `TEST_DATABASE_URL`).

## Backups and restore drill — DONE and VERIFIED (logical only)

- `scripts/pg_backup.sh`: `pg_dump -Fc` (one consistent snapshot, API stays up), piped straight into `openssl enc -aes-256-cbc -pbkdf2 -iter 600000`; plaintext never hits disk. An HMAC-SHA256 of the ciphertext is written next to it (CBC alone does not detect tampering). Passphrase comes from a file (`BACKUP_PASSPHRASE_FILE`); store it separately from the backups.
- `scripts/pg_restore_drill.sh`: verifies the HMAC, restores into a **fresh** database (never over the source), checks invariants (no ledger sequence gaps, no negative balances, no orphan accounts), compares a fingerprint (row counts, balance sum, md5 over all ledger rows) with the source, drops the drill database, prints the restore time.
- **Run on 2026-10-14** against the real stack (compose.postgres.yml, data created by `scripts/demo.sh`: 3 users, 2 colonies, 18 ledger rows, 1 completed trade): PASSED, restore took ~1 s. Negative runs all failed as intended: tampered file, wrong passphrase, and source modified after the backup (fingerprint diff on `balance_sum`).
- Limits: this proves a logical dump restores correctly on a tiny dataset. It says nothing about restore time at production size, RPO is the backup interval (not seconds), and there is no PITR, off-host copy, retention job or scheduled drill yet. Cron example is in the runbook.

## RPO / RTO (targets for PITR, still ASSUMED; only the logical-backup restore above was measured)

| | Target | How |
|---|---|---|
| RPO | <= 5 min (<= 1 min with synchronous replica) | WAL archiving (continuous) + streaming replica; PITR to any second within retention |
| RTO | <= 30 min regional restore; <= 2 min replica promotion | Documented runbook, tested quarterly |
| Backups | Daily base backup + continuous WAL, retained 14 days, encrypted client-side (age or KMS envelope) and stored off-host/off-account | A backup that lives only on the same host is not a backup |
| Per-colony failover | Each colony's rows are independent; losing one colony's primary does not stop the others once data is per-colony-partitioned. Until Phase 2 makes colonies separate trust domains, they share one database and one failover unit. | |

## Verification summary
VERIFIED: 3a items by tests (`phase3_test.go`, `regression_test.go` issue-3 tests now ungated and passing under `-race`), `go vet`, golangci-lint v2.5.0, backend Docker build, demo flow, and a real SIGTERM against the running binary (`shutdown complete`, port closed).
VERIFIED (3b backups): encrypted logical dump restored into a fresh database and checked, plus three negative cases.
ASSUMED: the rest of 3b and the PITR RPO/RTO numbers.





