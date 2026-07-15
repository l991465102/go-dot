.PHONY: build test migrate run-api

build:
	go build -o bin/go-dot .

test:
	go test ./...

migrate:
	go run . migrate

run-api:
	go run . api
