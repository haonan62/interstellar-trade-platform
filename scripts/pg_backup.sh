#!/usr/bin/env bash
# Encrypted logical backup of the PostgreSQL database (pg_dump -Fc).
#
#   BACKUP_PASSPHRASE_FILE=/secure/path/passphrase ./scripts/pg_backup.sh [outdir]
#
# Output: <outdir>/itp-<UTC timestamp>.dump.enc  (AES-256-CBC, PBKDF2 600k)
#         <outdir>/itp-<UTC timestamp>.dump.enc.hmac (HMAC-SHA256 of the ciphertext)
#
# This is a LOGICAL backup: RPO equals the backup interval. It is NOT PITR.
# Keep the passphrase and the backups in different places, off this host.
set -euo pipefail

OUT_DIR="${1:-./backups}"
: "${BACKUP_PASSPHRASE_FILE:?set BACKUP_PASSPHRASE_FILE (file containing the passphrase)}"
[ -s "$BACKUP_PASSPHRASE_FILE" ] || { echo "passphrase file empty/missing" >&2; exit 1; }

# Command that runs a process inside the db container; override for other setups.
DB_EXEC="${DB_EXEC:-docker compose -f compose.yml -f compose.postgres.yml exec -T db}"
DB_USER="${DB_USER:-itp}"
DB_NAME="${DB_NAME:-itp}"

umask 077
mkdir -p "$OUT_DIR"
ts="$(date -u +%Y%m%dT%H%M%SZ)"
out="$OUT_DIR/itp-$ts.dump.enc"
tmp="$out.partial"
trap 'rm -f "$tmp"' EXIT

# pg_dump runs inside one snapshot transaction, so the dump is consistent
# while the API keeps serving. Plaintext never touches the disk.
# shellcheck disable=SC2086
$DB_EXEC pg_dump -U "$DB_USER" -d "$DB_NAME" -Fc --no-owner \
  | openssl enc -aes-256-cbc -pbkdf2 -iter 600000 -salt -pass "file:$BACKUP_PASSPHRASE_FILE" -out "$tmp"

[ -s "$tmp" ] || { echo "empty backup, aborting" >&2; exit 1; }
mv "$tmp" "$out"
openssl dgst -sha256 -hmac "$(cat "$BACKUP_PASSPHRASE_FILE")" -r "$out" | cut -d' ' -f1 > "$out.hmac"
echo "backup written: $out ($(wc -c < "$out") bytes)"

