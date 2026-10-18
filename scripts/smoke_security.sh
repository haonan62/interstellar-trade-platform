#!/usr/bin/env bash
# Smoke test of the Phase 4/5 behaviours against a running stack on :8080.
B=http://localhost:8080
echo "-- bootstrap without token (want 403):"; curl -s -o /dev/null -w '%{http_code}\n' -X POST $B/api/v1/auth/bootstrap -H 'Content-Type: application/json' -d '{"username":"admin","password":"password-admin"}'
echo "-- bootstrap with token, cookie mode (want 201, empty token, Set-Cookie):"
curl -s -i -X POST $B/api/v1/auth/bootstrap -H 'Content-Type: application/json' -H 'X-Bootstrap-Token: tok-e2e-123' -H 'X-Session-Mode: cookie' -c /tmp/itp.jar -d '{"username":"admin","password":"password-admin"}' | grep -iE '^HTTP|set-cookie|"token"|x-request-id|content-security|x-frame'
echo "-- cookie GET me (want 200):"; curl -s -o /dev/null -w '%{http_code}\n' -b /tmp/itp.jar $B/api/v1/auth/me
echo "-- cookie POST without Origin (want 403):"; curl -s -o /dev/null -w '%{http_code}\n' -X POST -b /tmp/itp.jar $B/api/v1/colonies -H 'Content-Type: application/json' -d '{"name":"Earth"}'
echo "-- cookie POST with same Origin (want 201):"; curl -s -o /dev/null -w '%{http_code}\n' -X POST -b /tmp/itp.jar -H "Origin: $B" $B/api/v1/colonies -H 'Content-Type: application/json' -d '{"name":"Earth"}'
echo "-- CORS preflight from evil origin (want no allow-origin):"; curl -s -i -X OPTIONS $B/api/v1/users -H 'Origin: https://evil.example' | grep -ci 'access-control-allow-origin'
echo "-- 5 bad logins then right password (want 401 x6):"
for i in 1 2 3 4 5 6; do curl -s -o /dev/null -w '%{http_code} ' -X POST $B/api/v1/auth/login -H 'Content-Type: application/json' -d "{\"username\":\"admin\",\"password\":\"$([ $i -lt 6 ] && echo wrong-pass-$i || echo password-admin)\"}"; done; echo
echo "-- readyz:"; curl -s $B/api/v1/readyz
echo "-- metrics are NOT on the public port (want 0 itp_ series; the SPA fallback answers 200 HTML):"; curl -s $B/metrics | grep -c '^itp_'


