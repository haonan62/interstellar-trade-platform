#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"

json() {
  python - "$1" <<'PY'
import json,sys
print(json.loads(sys.argv[1]))
PY
}

extract() {
  python - "$1" "$2" <<'PY'
import json,sys
obj=json.loads(sys.argv[1])
for part in sys.argv[2].split('.'):
    obj=obj[part]
print(obj)
PY
}

BOOTSTRAP=$(curl -sS -X POST "$BASE_URL/api/v1/auth/bootstrap" -H 'Content-Type: application/json' -d '{"username":"admin","display_name":"Administrator","password":"ChangeMe123"}')
TOKEN=$(extract "$BOOTSTRAP" token)

EARTH=$(curl -sS -X POST "$BASE_URL/api/v1/colonies" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"name":"Earth"}')
MARS=$(curl -sS -X POST "$BASE_URL/api/v1/colonies" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"name":"Mars"}')
EARTH_ID=$(extract "$EARTH" id)
MARS_ID=$(extract "$MARS" id)

curl -sS -X POST "$BASE_URL/api/v1/colonies/$EARTH_ID/trust" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "{\"peer_colony_id\":\"$MARS_ID\"}" >/dev/null
curl -sS -X POST "$BASE_URL/api/v1/colonies/$MARS_ID/trust" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "{\"peer_colony_id\":\"$EARTH_ID\"}" >/dev/null

ALICE=$(curl -sS -X POST "$BASE_URL/api/v1/users" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "{\"username\":\"alice\",\"display_name\":\"Alice\",\"password\":\"Password123\",\"colony_id\":\"$EARTH_ID\",\"roles\":[\"colony_admin\",\"trader\",\"relay_operator\"]}")
BOB=$(curl -sS -X POST "$BASE_URL/api/v1/users" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "{\"username\":\"bob\",\"display_name\":\"Bob\",\"password\":\"Password123\",\"colony_id\":\"$MARS_ID\",\"roles\":[\"colony_admin\",\"trader\",\"relay_operator\"]}")
ALICE_ID=$(extract "$ALICE" id)
BOB_ID=$(extract "$BOB" id)

curl -sS -X POST "$BASE_URL/api/v1/accounts/mint" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "{\"colony_id\":\"$MARS_ID\",\"user_id\":\"$BOB_ID\",\"amount\":1000}" >/dev/null

ALICE_LOGIN=$(curl -sS -X POST "$BASE_URL/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"username":"alice","password":"Password123"}')
BOB_LOGIN=$(curl -sS -X POST "$BASE_URL/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"username":"bob","password":"Password123"}')
ALICE_TOKEN=$(extract "$ALICE_LOGIN" token)
BOB_TOKEN=$(extract "$BOB_LOGIN" token)

TRADE=$(curl -sS -X POST "$BASE_URL/api/v1/trades/offers" -H "Authorization: Bearer $ALICE_TOKEN" -H 'Content-Type: application/json' -d "{\"seller_user_id\":\"$ALICE_ID\",\"buyer_user_id\":\"$BOB_ID\",\"asset\":\"design-v1\",\"price\":150}")
TRADE_ID=$(extract "$TRADE" id)

EARTH_BUNDLE=$(curl -sS -X POST "$BASE_URL/api/v1/relay/export" -H "Authorization: Bearer $ALICE_TOKEN" -H 'Content-Type: application/json' -d "{\"colony_id\":\"$EARTH_ID\",\"to_colony_id\":\"$MARS_ID\"}")
curl -sS -X POST "$BASE_URL/api/v1/relay/import" -H "Authorization: Bearer $BOB_TOKEN" -H 'Content-Type: application/json' -d "{\"colony_id\":\"$MARS_ID\",\"bundle\":$EARTH_BUNDLE}" >/dev/null
curl -sS -X POST "$BASE_URL/api/v1/trades/$TRADE_ID/accept" -H "Authorization: Bearer $BOB_TOKEN" >/dev/null
MARS_BUNDLE=$(curl -sS -X POST "$BASE_URL/api/v1/relay/export" -H "Authorization: Bearer $BOB_TOKEN" -H 'Content-Type: application/json' -d "{\"colony_id\":\"$MARS_ID\",\"to_colony_id\":\"$EARTH_ID\"}")
curl -sS -X POST "$BASE_URL/api/v1/relay/import" -H "Authorization: Bearer $ALICE_TOKEN" -H 'Content-Type: application/json' -d "{\"colony_id\":\"$EARTH_ID\",\"bundle\":$MARS_BUNDLE}" >/dev/null
SETTLEMENT_BUNDLE=$(curl -sS -X POST "$BASE_URL/api/v1/relay/export" -H "Authorization: Bearer $ALICE_TOKEN" -H 'Content-Type: application/json' -d "{\"colony_id\":\"$EARTH_ID\",\"to_colony_id\":\"$MARS_ID\"}")
curl -sS -X POST "$BASE_URL/api/v1/relay/import" -H "Authorization: Bearer $BOB_TOKEN" -H 'Content-Type: application/json' -d "{\"colony_id\":\"$MARS_ID\",\"bundle\":$SETTLEMENT_BUNDLE}" >/dev/null

curl -sS "$BASE_URL/api/v1/trades" -H "Authorization: Bearer $TOKEN"
