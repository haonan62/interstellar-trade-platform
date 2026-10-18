# Interstellar Trade Platform — Deployment Runbook
## Single VM · Docker · nginx reverse proxy · Let's Encrypt TLS

---

## Prerequisites

- Ubuntu 22.04 / 24.04 VM (any cloud provider)
- A domain name (`yourdomain.com`) with an A record pointing to the VM's public IP
- Ports 22 (SSH), 80 (HTTP), and 443 (HTTPS) open in your firewall / security group
- The VM's internal port 8080 must NOT be open to the internet — the compose file
  already binds it to `127.0.0.1` only, so it is only reachable from the host itself

---

## 1. Prepare the VM

```bash
# Update system packages
sudo apt update && sudo apt upgrade -y

# Install Docker
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER   # log out and back in for this to take effect

# Install Docker Compose plugin (included with Docker Engine >= 23)
docker compose version           # confirm it works

# Install nginx and Certbot
sudo apt install -y nginx certbot python3-certbot-nginx

# Open the firewall (ufw)
sudo ufw allow 'Nginx Full'      # opens 80 and 443
sudo ufw allow OpenSSH
sudo ufw enable
```

---

## 2. Deploy the application

```bash
# Clone or copy the repository to the VM
git clone https://github.com/yourorg/interstellar-trade-platform.git
cd interstellar-trade-platform

# Create your .env file from the template
cp .env.example .env
nano .env                        # set BOOTSTRAP_TOKEN (openssl rand -base64 24); leave CORS_ORIGINS empty
                                 # for the bundled UI (same origin, CORS fails closed)

# Build the Docker image (takes ~2–3 minutes on first run)
docker compose build

# Start the container in the background
docker compose up -d

# Confirm it is healthy
docker compose ps
docker compose logs -f trade     # watch logs; Ctrl-C to stop following
```

The container is now listening on `127.0.0.1:8080`. Nothing outside the VM
can reach it yet.

---

## 3. nginx step 1: HTTP-only config, to obtain the certificate

`deploy/nginx.conf` refers to certificate files that do not exist yet, so nginx
would refuse to load it. Start with the HTTP-only bootstrap config.

```bash
sed -i 's/yourdomain.com/YOUR_ACTUAL_DOMAIN/g' deploy/nginx.conf deploy/nginx-http-bootstrap.conf
sudo mkdir -p /var/www/certbot
sudo cp deploy/nginx-http-bootstrap.conf /etc/nginx/sites-available/interstellar-trade
sudo ln -sf /etc/nginx/sites-available/interstellar-trade /etc/nginx/sites-enabled/interstellar-trade
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t && sudo systemctl reload nginx
```

---

## 4. Certificate, then the full nginx config

```bash
# webroot mode: renewals keep working while nginx runs (no port-80 conflict)
sudo certbot certonly --webroot -w /var/www/certbot -d YOUR_ACTUAL_DOMAIN \
     --deploy-hook "systemctl reload nginx"
sudo certbot renew --dry-run

sudo cp deploy/nginx.conf /etc/nginx/sites-available/interstellar-trade
sudo nginx -t && sudo systemctl reload nginx
```

Your site is now live at `https://yourdomain.com`. (`./scripts/nginx_check.sh`
runs `nginx -t` on both files in a container, before you touch the server.)

### First login (bootstrap)

The first administrator can only be created with the one-time `BOOTSTRAP_TOKEN`
(sent as the `X-Bootstrap-Token` header; the UI has a field for it). If you left
it empty, the server generated one and logged it once:
`docker compose logs trade | grep bootstrap_token`. It is useless once the
system is bootstrapped. Then enrol MFA for the admin (UI/API:
`POST /api/v1/auth/mfa/enroll` then `/confirm`) and set `REQUIRE_ADMIN_MFA=true`
in `.env` and `docker compose up -d`.

---

## 5. Verify the security posture

```bash
# Confirm the container port is NOT accessible from outside
# (this should time out or refuse from any external machine)
curl http://YOUR_VM_PUBLIC_IP:8080/api/v1/healthz   # must fail

# Confirm HTTPS works
curl https://yourdomain.com/api/v1/healthz           # must return {"status":"ok"}

# Check TLS grade (aim for A or A+)
# Visit: https://www.ssllabs.com/ssltest/analyze.html?d=yourdomain.com

# Check security headers
curl -I https://yourdomain.com
# Look for: Strict-Transport-Security, X-Frame-Options, X-Content-Type-Options
```

---

## 6. Day-2 operations

### View logs
```bash
docker compose logs -f trade                 # application logs
sudo tail -f /var/log/nginx/interstellar-trade.access.log
sudo tail -f /var/log/nginx/interstellar-trade.error.log
```

### Update the application

```bash
git pull
docker compose build --no-cache
docker compose up -d --force-recreate trade
# The named volume (trade-data) is preserved; no data is lost.
```

### Backup the state file

```bash
# The state file lives in the Docker named volume.
# Find the actual path:
docker volume inspect interstellar-trade_trade-data | grep Mountpoint

# Copy it out
sudo cp /var/lib/docker/volumes/interstellar-trade_trade-data/_data/state.json \
        ~/backups/state-$(date +%Y%m%d-%H%M%S).json
```

Automate this with a cron job:

```cron
# /etc/cron.d/interstellar-trade-backup
0 3 * * * root cp /var/lib/docker/volumes/interstellar-trade_trade-data/_data/state.json \
    /home/ubuntu/backups/state-$(date +\%Y\%m\%d).json 2>&1 | logger -t trade-backup
```

### Restore from backup

```bash
docker compose stop trade
sudo cp ~/backups/state-YYYYMMDD.json \
    /var/lib/docker/volumes/interstellar-trade_trade-data/_data/state.json
docker compose start trade
```

### PostgreSQL: encrypted backup and restore drill

(Only when running with `compose.postgres.yml`; the section above is for the file store.)

```bash
export BACKUP_PASSPHRASE_FILE=/root/.itp-backup-pass   # chmod 600; keep a copy off-host
./scripts/pg_backup.sh /var/backups/itp                # encrypted dump + .hmac
./scripts/pg_restore_drill.sh /var/backups/itp/itp-<ts>.dump.enc
```

```cron
0 3 * * * root cd /opt/interstellar-trade-platform && BACKUP_PASSPHRASE_FILE=/root/.itp-backup-pass ./scripts/pg_backup.sh /var/backups/itp 2>&1 | logger -t itp-backup
```

Copy the `.enc` and `.hmac` files off the host (a backup on the same host is not a backup) and run the drill regularly. The drill compares with the live database, so run it right after a backup or set `SKIP_SOURCE_COMPARE=1`. To recover for real: stop `trade`, restore the decrypted dump into an empty database with `pg_restore`, start `trade`.

### Users: deactivate, roles, password reset, lost MFA

All require an administrator (colony admins only within their colony; only a
super_admin can touch a super_admin; nobody can modify their own account here).

| Need | Call |
|---|---|
| Deactivate / reactivate (revokes all sessions) | `POST /api/v1/users/{id}/active` `{"active": false}` |
| Change roles | `POST /api/v1/users/{id}/roles` `{"roles": ["trader"]}` |
| Forgotten password / locked account | `POST /api/v1/users/{id}/reset-password` returns a one-time `reset_token` (valid 1 h); hand it to the user, who calls `POST /api/v1/auth/reset-password` `{"reset_token","new_password"}`. Clears the lockout, revokes sessions. |
| Lost authenticator | `POST /api/v1/users/{id}/mfa/reset`, user enrols again |

Accounts lock for 15 minutes after 5 consecutive failed logins (password or MFA code).

### Monitoring and logs

- Logs are JSON on stdout (`docker compose logs trade`). Security events carry `"audit":true`,
  `event`, `outcome`, `request_id`, `trace_id`, `user_id`, `ip`. Filter:
  `docker compose logs trade | grep '"audit":true'`. nginx forwards `X-Request-Id` and `traceparent`.
- Metrics: Prometheus text on `:9090/metrics` **inside the compose network only** (not published,
  not proxied). Scrape from a container on the same network. Useful series:
  `itp_http_requests_total`, `itp_http_request_duration_seconds`, `itp_login_failures_total`,
  `itp_audit_events_total{event="csrf.blocked"}`.
- Probes: `/api/v1/healthz` (process up), `/api/v1/readyz` (store reachable). The container
  HEALTHCHECK runs `/interstellar-trade --healthcheck` (no shell/wget in the image).
- Volumes created by an older (root) image: `docker run --rm -v <vol>:/data busybox chown 65532:65532 /data`.

### Hard restart

```bash
docker compose restart trade
```

### Stop everything

```bash
docker compose down                  # stops container, volume is kept
docker compose down -v               # WARNING: deletes the volume and all data
```

---

## 7. Firewall reference

| Port | Protocol | Bound to          | Reachable from | Purpose              |
|------|----------|-------------------|----------------|----------------------|
| 22   | TCP      | 0.0.0.0           | Internet       | SSH management       |
| 80   | TCP      | 0.0.0.0           | Internet       | HTTP → HTTPS redirect|
| 443  | TCP      | 0.0.0.0           | Internet       | HTTPS (nginx)        |
| 8080 | TCP      | 127.0.0.1 only    | Host only      | Go backend (Docker)  |

Port 8080 is never exposed to the internet. The compose file enforces this with
`ports: ["127.0.0.1:8080:8080"]`.

---

## 8. Troubleshooting

| Symptom | Check |
|---------|-------|
| 502 Bad Gateway | `docker compose ps` — is the container running? `docker compose logs trade` |
| 404 on all routes | nginx config installed? `sudo nginx -t` |
| CORS errors in browser | The bundled UI is same-origin and needs no CORS. Only a separate front-end origin must be listed in `CORS_ORIGINS` (exact origin, no trailing slash). Empty = none allowed. |
| 403 "cross-origin request refused" | Cookie-authenticated POSTs need an `Origin` equal to `Host`; the nginx config passes `Host $http_host`. Check a proxy in front of nginx is not rewriting Host. |
| 403 `mfa_enrollment_required` | `REQUIRE_ADMIN_MFA=true` and this admin has no MFA: enrol via `/auth/mfa/enroll` + `/confirm`. |
| 429 on login | nginx per-IP limit (10/min) or see lockout above. |
| Certificate renewal failing | `sudo certbot renew --dry-run` — port 80 must be open |
| State file not persisting | `docker volume inspect interstellar-trade_trade-data` — check Mountpoint exists |
