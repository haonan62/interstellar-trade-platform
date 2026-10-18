# Base images are pinned by digest so a build is reproducible and a moved tag
# cannot change what we ship. Dependabot (docker ecosystem) proposes updates.

# ─── Stage 1: Build the React frontend ───────────────────────────────────────
FROM node:22-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402 AS frontend-builder

WORKDIR /app/frontend
# package-lock.json must be committed: `npm ci` fails without it.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --prefer-offline

COPY frontend/ ./
# The Go server serves the API and the static files on one origin, so the
# frontend uses relative paths (/api/v1/...).
RUN npm run build


# ─── Stage 2: Build the Go backend ───────────────────────────────────────────
# Must be >= the "go" directive in backend/go.mod (GOTOOLCHAIN=local in this image).
FROM golang:1.25-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS backend-builder

WORKDIR /app/backend
# go.sum pins the module versions (pgx, x/crypto).
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /interstellar-trade .
# An empty data directory owned by the runtime user (copied into the image below).
RUN mkdir /data-skel


# ─── Stage 3: Minimal runtime image ──────────────────────────────────────────
FROM scratch

# TLS root certs (needed for sslmode=verify-full database connections).
COPY --from=backend-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=backend-builder /interstellar-trade /interstellar-trade
COPY --from=frontend-builder /app/frontend/dist /frontend

# /data is the mount point of the state volume (file store only). A new named
# volume inherits this ownership; for a volume created by an older (root)
# image run once:  docker run --rm -v <volume>:/data busybox chown 65532:65532 /data
COPY --from=backend-builder --chown=65532:65532 /data-skel /data

# Run as an unprivileged numeric user (scratch has no /etc/passwd; numeric works).
USER 65532:65532

EXPOSE 8080

# There is no shell, wget or curl in this image: the binary probes itself.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/interstellar-trade", "--healthcheck"]

# Configuration comes from the environment (see .env.example): CORS_ORIGINS,
# DATABASE_URL, BOOTSTRAP_TOKEN, REQUIRE_ADMIN_MFA, METRICS_ADDR, LOG_LEVEL.
ENTRYPOINT ["/interstellar-trade"]
CMD ["--addr", ":8080", "--data", "/data/state.json", "--static", "/frontend"]

