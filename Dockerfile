# ─── Stage 1: Build the React frontend ───────────────────────────────────────
FROM node:22-alpine AS frontend-builder

WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --prefer-offline

COPY frontend/ ./
# VITE_API_BASE_URL is empty: the Go server serves both the API and the static
# files on the same origin, so the frontend uses relative paths (/api/v1/...).
RUN npm run build


# ─── Stage 2: Build the Go backend ───────────────────────────────────────────
FROM golang:1.22-alpine AS backend-builder

WORKDIR /app/backend
COPY backend/go.mod ./
# No external dependencies — go.sum may not exist.
RUN go mod download || true

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /interstellar-trade .


# ─── Stage 3: Minimal runtime image ──────────────────────────────────────────
FROM scratch

# TLS root certs (needed if the Go binary ever makes outbound HTTPS calls).
COPY --from=backend-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# The compiled binary.
COPY --from=backend-builder /interstellar-trade /interstellar-trade

# Built frontend assets served as static files by the Go binary.
COPY --from=frontend-builder /app/frontend/dist /frontend

# Data directory is a mount point — nothing is baked in.
# The host mounts a named volume at /data (see compose.yml).

EXPOSE 8080

# --cors is set to the public HTTPS origin so that browsers talking to the
# API from the same origin (nginx terminates TLS, proxies to this port)
# match the CORS allowlist. If your domain uses www, add it too.
# The actual value is overridden by CORS_ORIGINS env var via the entrypoint.
ENTRYPOINT ["/interstellar-trade"]
CMD ["--addr", ":8080", \
     "--data", "/data/state.json", \
     "--static", "/frontend", \
     "--cors", "https://yourdomain.com"]
