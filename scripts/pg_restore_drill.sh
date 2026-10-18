#!/usr/bin/env bash
# Restore drill: decrypt a backup, restore it into a FRESH database, and prove
# the restored data equals the source (row counts, money, ledger hashes).
#
#   BACKUP_PASSPHRASE_FILE=... ./scripts/pg_restore_drill.sh backups/itp-XXXX.dump.enc
#
# Run it right after a backup while the source has not changed, or set
# SKIP_SOURCE_COMPARE=1 to only run the internal invariants on the restore.
# Prints the measured restore duration (the only RTO number that is evidence).
set -euo pipefail

BACKUP="${1:?usage: pg_restore_drill.sh <backup.dump.enc>}"
: "${BACKUP_PASSPHRASE_FILE:?set BACKUP_PASSPHRASE_FILE}"
DB_EXEC="${DB_EXEC:-docker compose -f compose.yml -f compose.postgres.yml exec -T db}"
DB_USER="${DB_USER:-itp}"
DB_NAME="${DB_NAME:-itp}"
DRILL_DB="itp_drill_$$"

psql_src() { $DB_EXEC psql -U "$DB_USER" -d "$DB_NAME" -qtAX -v ON_ERROR_STOP=1 "$@"; }
psql_drill() { $DB_EXEC psql -U "$DB_USER" -d "$DRILL_DB" -qtAX -v ON_ERROR_STOP=1 "$@"; }

cleanup() { psql_src -c "DROP DATABASE IF EXISTS $DRILL_DB" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# 1. Integrity/authenticity of the file before decrypting anything.
expected="$(cat "$BACKUP.hmac")"
actual="$(openssl dgst -sha256 -hmac "$(cat "$BACKUP_PASSPHRASE_FILE")" -r "$BACKUP" | cut -d' ' -f1)"
[ "$expected" = "$actual" ] || { echo "FAIL: HMAC mismatch (corrupt, tampered, or wrong passphrase)" >&2; exit 1; }
echo "ok  hmac verified"

# Fingerprint: stable text summarising everything that matters.
FINGERPRINT_SQL="
SELECT 'colonies='||count(*) FROM colonies
UNION ALL SELECT 'users='||count(*) FROM users
UNION ALL SELECT 'accounts='||count(*)||' balance_sum='||coalesce(sum(balance),0) FROM accounts
UNION ALL SELECT 'trades='||count(*) FROM trades
UNION ALL SELECT 'ledger='||count(*)||' md5='||coalesce(md5(string_agg(colony_id||':'||seq||':'||doc::text, '|' ORDER BY colony_id, seq)),'-') FROM ledger_entries
UNION ALL SELECT 'outbox='||count(*) FROM outbox
UNION ALL SELECT 'processed_envelopes='||count(*) FROM processed_envelopes
UNION ALL SELECT 'bundles='||count(*) FROM bundles
ORDER BY 1;"

start=$(date +%s)

# 2. Restore into a fresh database (never over the source).
psql_src -c "CREATE DATABASE $DRILL_DB" >/dev/null
openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 -pass "file:$BACKUP_PASSPHRASE_FILE" -in "$BACKUP" \
  | $DB_EXEC pg_restore -U "$DB_USER" -d "$DRILL_DB" --no-owner --exit-on-error
end=$(date +%s)
echo "ok  restored into fresh database $DRILL_DB in $((end - start))s"

# 3. Invariants that must hold on the restored copy alone.
bad_ledger_gaps="$(psql_drill -c "SELECT count(*) FROM (SELECT colony_id, count(*) c, max(seq) m, min(seq) n FROM ledger_entries GROUP BY colony_id) t WHERE m - n + 1 <> c")"
neg_balances="$(psql_drill -c "SELECT count(*) FROM accounts WHERE balance < 0")"
orphans="$(psql_drill -c "SELECT count(*) FROM accounts a LEFT JOIN users u ON u.id=a.user_id WHERE u.id IS NULL")"
[ "$bad_ledger_gaps" = 0 ] || { echo "FAIL: ledger sequence gaps in $bad_ledger_gaps colonies" >&2; exit 1; }
[ "$neg_balances" = 0 ] || { echo "FAIL: negative balances" >&2; exit 1; }
[ "$orphans" = 0 ] || { echo "FAIL: orphan accounts" >&2; exit 1; }
echo "ok  invariants (no ledger gaps, no negative balances, no orphans)"

# 4. Compare with the live source.
restored="$(psql_drill -c "$FINGERPRINT_SQL")"
echo "$restored" | sed 's/^/    /'
if [ "${SKIP_SOURCE_COMPARE:-0}" != 1 ]; then
  source_fp="$(psql_src -c "$FINGERPRINT_SQL")"
  if [ "$restored" = "$source_fp" ]; then
    echo "ok  restored fingerprint equals source"
  else
    echo "FAIL: restored data differs from source (was the source written since the backup?)" >&2
    diff <(echo "$source_fp") <(echo "$restored") >&2 || true
    exit 1
  fi
fi
echo "DRILL PASSED"

