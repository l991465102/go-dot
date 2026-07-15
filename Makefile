.PHONY: build test run-api

build:
	go build -o bin/go-dot .

test:
	go test ./...

run-api:
	go run . api
