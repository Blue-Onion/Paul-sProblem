.PHONY: run build test

run:
	go run ./cmd/server

build:
	go build -o bin/attendance-extractor ./cmd/server

test:
	go test ./...
