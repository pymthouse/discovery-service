.PHONY: build test run compose-up compose-down compose-logs hooks

build:
	go build -o bin/discoveryd ./cmd/discoveryd

test:
	go test ./...

run:
	go run ./cmd/discoveryd

compose-up:
	docker compose up -d --build

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f discovery postgres

# Postgres on host :5433 for local go run (see docker-compose.dev.yml)
compose-postgres-dev:
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d postgres

hooks:
	bash .githooks/install.sh
