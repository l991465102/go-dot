.PHONY: build test test-go test-web test-integration test-e2e test-e2e-bootstrap test-fault load-test migrate run-api run-worker run-scheduler compose-config

build:
	go build -o bin/go-dot .

test: test-go test-web

test-go:
	go test ./...

test-web:
	npm --prefix web test -- --run

test-integration:
	go test -tags=integration -count=1 -timeout=3m ./tests/integration

test-e2e:
	@if [ "$${GO_DOT_E2E_BOOTSTRAP:-}" = "1" ]; then bash tests/e2e/bootstrap.sh; fi
	npm --prefix web run test:e2e

test-e2e-bootstrap:
	bash tests/e2e/bootstrap.sh

test-fault:
	bash tests/fault/clickhouse_recovery.sh
	bash tests/fault/dlq.sh

load-test:
	k6 run tests/load/collect.js

migrate:
	go run . migrate

run-api:
	go run . api

run-worker:
	go run . worker

run-scheduler:
	go run . scheduler

compose-config:
	docker compose config
