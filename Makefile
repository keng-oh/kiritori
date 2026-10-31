.PHONY: run test build

run:
	go run ./cmd/kiritori

test:
	go test ./...

build:
	go build -o bin/kiritori ./cmd/kiritori
