#!/usr/bin/env bash
# Validates deploy/nginx.conf and deploy/nginx-http-bootstrap.conf with the real
# nginx binary (`nginx -t`). Needs docker and openssl. Used locally and in CI.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="${NGINX_IMAGE:-nginx:1.27-alpine@sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# The site file is meant to be included from http {}; a throwaway cert stands in
# for the Let's Encrypt one so ssl_certificate paths resolve.
mkdir -p "$TMP/live/yourdomain.com"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=yourdomain.com" \
  -keyout "$TMP/live/yourdomain.com/privkey.pem" -out "$TMP/live/yourdomain.com/fullchain.pem" 2>/dev/null
printf 'events {}\nhttp {\n  include /etc/nginx/site.conf;\n}\n' > "$TMP/nginx.conf"

for site in nginx.conf nginx-http-bootstrap.conf; do
  echo "== nginx -t: deploy/$site"
  docker run --rm \
    -v "$TMP/nginx.conf:/etc/nginx/nginx.conf:ro" \
    -v "$ROOT/deploy/$site:/etc/nginx/site.conf:ro" \
    -v "$TMP/live:/etc/letsencrypt/live:ro" \
    "$IMAGE" nginx -t
done

