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
nano .env                        # set CORS_ORIGINS=https://yourdomain.com

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

## 3. Configure nginx

```bash
# Replace the placeholder domain in the nginx config
sed -i 's/yourdomain.com/YOUR_ACTUAL_DOMAIN/g' deploy/nginx.conf

# Install the config
sudo cp deploy/nginx.conf /etc/nginx/sites-available/interstellar-trade
sudo ln -s /etc/nginx/sites-available/interstellar-trade \
           /etc/nginx/sites-enabled/interstellar-trade

# Remove the default site if it exists
sudo rm -f /etc/nginx/sites-enabled/default

# Test the config
sudo nginx -t

# Start / reload nginx
sudo systemctl reload nginx
```

At this point `http://yourdomain.com` should serve the app (HTTP, no TLS yet).

---

## 4. Obtain a TLS certificate (Let's Encrypt)

```bash
# Certbot will edit your nginx config to add the ssl_certificate lines
# and create an automatic renewal cron job.
sudo certbot --nginx -d yourdomain.com

# Follow the prompts — provide an email for expiry warnings.
# When asked whether to redirect, choose: 2 (redirect all HTTP to HTTPS)
# (the nginx config already does this, so either answer is fine)

# Verify auto-renewal works
sudo certbot renew --dry-run
```

Your site is now live at `https://yourdomain.com`.

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
| CORS errors in browser | Is `CORS_ORIGINS` in `.env` set to the exact HTTPS URL including no trailing slash? |
| Certificate renewal failing | `sudo certbot renew --dry-run` — port 80 must be open |
| State file not persisting | `docker volume inspect interstellar-trade_trade-data` — check Mountpoint exists |
