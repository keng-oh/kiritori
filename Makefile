.PHONY: run test build fmt-check vet check

run:
	go run ./cmd/kiritori

test:
	go test ./...

build:
	go build -o bin/kiritori ./cmd/kiritori

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

check: fmt-check vet test
