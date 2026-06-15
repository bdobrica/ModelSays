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
GOOSE := go run github.com/pressly/goose/v3/cmd/goose@latest

.PHONY: help start dev postgres-up postgres-down postgres-logs postgres-reset migrate-up migrate-down backend client test-backend test-client

help:
	@printf '%s\n' \
		'Model Says development targets:' \
		'  .env support       Root .env values are loaded automatically when present' \
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
		'  make test-backend  Run backend tests' \
		'  make test-client   Run client tests'

start: dev

dev: postgres-up migrate-up
	@trap 'kill 0' INT TERM EXIT; \
	(cd $(BACKEND_DIR) && DATABASE_URL='$(DATABASE_URL)' HTTP_ADDR='$(HTTP_ADDR)' APP_ENV='$(APP_ENV)' CORS_ALLOWED_ORIGINS='$(CORS_ALLOWED_ORIGINS)' go run ./cmd/server) & \
	(cd $(CLIENT_DIR) && npm run dev) & \
	wait

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

test-backend:
	cd $(BACKEND_DIR) && go test ./...

test-client:
	cd $(CLIENT_DIR) && npm run test
