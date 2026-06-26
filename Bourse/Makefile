.PHONY: help tidy migrate run-api run-worker up down logs loadtest fmt

DATABASE_URL ?= postgres://bourse:bourse@localhost:5432/bourse?sslmode=disable
REDIS_URL    ?= redis://localhost:6379/0

help:
	@echo "Targets:"
	@echo "  tidy        - go mod tidy (generates go.sum)"
	@echo "  migrate     - apply SQL migrations to \$$DATABASE_URL"
	@echo "  run-api     - run the API server locally"
	@echo "  run-worker  - run the worker locally"
	@echo "  up          - docker compose up (full stack)"
	@echo "  down        - docker compose down -v"
	@echo "  loadtest    - run the concurrency load test (needs API running)"

tidy:
	go mod tidy

migrate:
	psql "$(DATABASE_URL)" -f migrations/0001_init.sql

run-api:
	DATABASE_URL="$(DATABASE_URL)" REDIS_URL="$(REDIS_URL)" go run ./cmd/api

run-worker:
	DATABASE_URL="$(DATABASE_URL)" REDIS_URL="$(REDIS_URL)" go run ./cmd/worker

up:
	docker compose -f deploy/docker-compose.yml up --build

down:
	docker compose -f deploy/docker-compose.yml down -v

loadtest:
	go run ./loadtest

fmt:
	go fmt ./...
