SHELL := /bin/bash

ENV_FILE ?= .env

ifneq (,$(wildcard $(ENV_FILE)))
include $(ENV_FILE)
export $(shell sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' $(ENV_FILE))
endif

BACKEND_DIR := backend
CLIENT_DIR := client
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/modelsays?sslmode=disable
HTTP_ADDR ?= :8080
APP_ENV ?= development
CORS_ALLOWED_ORIGINS ?= http://localhost:5173
GOOSE_VERSION := v3.27.2
GOOSE := go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)

.PHONY: help install bootstrap start dev postgres-up postgres-down postgres-logs postgres-reset migrate-up migrate-down backend client pi-build pi-run pi-install baseline ops-backup ops-restore ops-retention format check-format vet-backend test-backend test-client build-client check verify

help:
	@printf '%s\n' \
		'Model Says development targets:' \
		'  .env support       Root .env values are loaded automatically when present' \
		'  make install       Install dependencies and start the complete local game' \
		'  make bootstrap     Download locked Go and npm dependencies' \
		'  make start         Start Postgres, apply migrations, then run backend and client together' \
		'  make dev           Alias for start' \
		'  make postgres-up   Start local PostgreSQL with Docker Compose' \
		'  make postgres-down Stop local PostgreSQL containers' \
		'  make postgres-logs Tail PostgreSQL logs' \
		'  make postgres-reset Stop PostgreSQL and delete its volume' \
		'  make migrate-up    Apply backend SQL migrations' \
		'  make migrate-down  Roll back the most recent backend migration' \
		'  make backend       Run the backend only' \
		'  make client        Run the client only' \
		'  make pi-build      Build reusable ARM/Linux deployment artifacts' \
		'  make pi-run        Run the prebuilt private-LAN Pi deployment' \
		'  make pi-install    Install/update the app and boot service on a Pi' \
		'  make baseline      Measure 3-, 8-, and 12-player PostgreSQL gameplay workloads' \
		'  make ops-backup    Create a timestamped PostgreSQL custom-format backup' \
		'  make ops-restore   Restore BACKUP_FILE into disposable RESTORE_DATABASE (confirmation required)' \
		'  make ops-retention Preview retention cleanup; set APPLY_RETENTION=yes to execute' \
		'  make format        Format backend Go source' \
		'  make check-format  Check backend Go formatting' \
		'  make vet-backend   Run Go static analysis' \
		'  make test-backend  Run backend tests' \
		'  make test-client   Run client tests' \
		'  make build-client  Type-check and build the client' \
		'  make check         Check formatting, tests, and client build' \
		'  make verify        Alias for check'

install: bootstrap
	$(MAKE) start

bootstrap:
	cd $(BACKEND_DIR) && go mod download
	cd $(CLIENT_DIR) && npm ci

start: dev

dev: postgres-up migrate-up
	@(cd $(BACKEND_DIR) && DATABASE_URL='$(DATABASE_URL)' HTTP_ADDR='$(HTTP_ADDR)' APP_ENV='$(APP_ENV)' CORS_ALLOWED_ORIGINS='$(CORS_ALLOWED_ORIGINS)' go run ./cmd/server) & backend_pid=$$!; \
	(cd $(CLIENT_DIR) && npm run dev) & client_pid=$$!; \
	trap 'kill $$backend_pid $$client_pid 2>/dev/null || true' INT TERM EXIT; \
	wait -n $$backend_pid $$client_pid; status=$$?; \
	kill $$backend_pid $$client_pid 2>/dev/null || true; \
	wait $$backend_pid $$client_pid 2>/dev/null || true; \
	exit $$status

postgres-up:
	docker compose up -d postgres

postgres-down:
	docker compose down

postgres-logs:
	docker compose logs -f postgres

postgres-reset:
	docker compose down -v

migrate-up:
	cd $(BACKEND_DIR) && $(GOOSE) -dir migrations postgres '$(DATABASE_URL)' up

migrate-down:
	cd $(BACKEND_DIR) && $(GOOSE) -dir migrations postgres '$(DATABASE_URL)' down

backend:
	cd $(BACKEND_DIR) && DATABASE_URL='$(DATABASE_URL)' HTTP_ADDR='$(HTTP_ADDR)' APP_ENV='$(APP_ENV)' CORS_ALLOWED_ORIGINS='$(CORS_ALLOWED_ORIGINS)' go run ./cmd/server

client:
	cd $(CLIENT_DIR) && npm run dev

pi-build: bootstrap build-client
	mkdir -p bin
	cd $(BACKEND_DIR) && go build -o ../bin/modelsays ./cmd/server
	cd $(BACKEND_DIR) && GOBIN='$(abspath bin)' go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)

pi-run:
	bash ./scripts/run-pi.sh

pi-install:
	bash ./scripts/install-pi.sh

baseline: build-client
	cd $(BACKEND_DIR) && go run ./cmd/baseline -database-url '$(DATABASE_URL)' -client-dist ../client/dist

ops-backup:
	bash ./scripts/ops-backup.sh '$(DATABASE_URL)' '$(BACKUP_DIR)'

ops-restore:
	bash ./scripts/ops-restore.sh '$(DATABASE_URL)' '$(BACKUP_FILE)' '$(RESTORE_DATABASE)' '$(CONFIRM_RESTORE)'

ops-retention:
	bash ./scripts/ops-retention.sh '$(DATABASE_URL)' '$(RETENTION_DAYS)' '$(APPLY_RETENTION)'

format:
	gofmt -w $$(find $(BACKEND_DIR) -type f -name '*.go')

check-format:
	@test -z "$$(gofmt -l $$(find $(BACKEND_DIR) -type f -name '*.go'))" || { \
		printf '%s\n' 'Go files need formatting. Run make format:'; \
		gofmt -l $$(find $(BACKEND_DIR) -type f -name '*.go'); \
		exit 1; \
	}

test-backend:
	cd $(BACKEND_DIR) && go test ./...

vet-backend:
	cd $(BACKEND_DIR) && go vet ./...

test-client:
	cd $(CLIENT_DIR) && npm run test

build-client:
	cd $(CLIENT_DIR) && npm run build

check: check-format vet-backend test-backend test-client build-client

verify: check
