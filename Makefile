# Common development tasks. Run `make help` for the list.
.DEFAULT_GOAL := help
SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BOOTSTRAP_TOKEN ?= dev-token

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---- development ----
.PHONY: dev-backend dev-frontend
dev-backend: ## Run the API on :8080 with the JSON file store
	cd backend && BOOTSTRAP_TOKEN=$(BOOTSTRAP_TOKEN) go run ./cmd/itp-server -addr :8080 -data ./data/state.json

dev-frontend: ## Run the Vite dev server on :5173 (proxies /api to :8080)
	cd frontend && npm run dev

## ---- quality ----
.PHONY: fmt lint test test-pg check
fmt: ## Format Go code
	cd backend && gofmt -w .

lint: ## Lint backend (gofmt + vet) and frontend (eslint)
	cd backend && test -z "$$(gofmt -l .)" && go vet ./...
	cd frontend && npm run lint

test: ## Run backend (race, file store) and frontend tests
	cd backend && go test -race -count=1 ./...
	cd frontend && npm test

test-pg: ## Run the backend suite against PostgreSQL (TEST_DATABASE_URL must be set)
	cd backend && go test -race -count=1 ./...

check: lint test ## Everything CI runs locally (except image scans)

## ---- build ----
.PHONY: build build-backend build-frontend image
build: build-frontend build-backend ## Build the frontend bundle and the server binary

build-backend: ## Build backend/bin/itp-server
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/itp-server ./cmd/itp-server

build-frontend: ## Build frontend/dist
	cd frontend && npm ci && npm run build

image: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t interstellar-trade:$(VERSION) -t interstellar-trade:latest .

.PHONY: run
run: build ## Run the built server with the built UI on :8080
	BOOTSTRAP_TOKEN=$(BOOTSTRAP_TOKEN) ./backend/bin/itp-server -addr :8080 -data ./backend/data/state.json -static ./frontend/dist
