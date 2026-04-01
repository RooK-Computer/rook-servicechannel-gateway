.PHONY: build test test-e2e verify run

build:
	go build ./...

test:
	go test ./...

test-e2e:
	go test ./tests/e2e -v

verify:
	go test ./... && go build ./...

run:
	go run ./cmd/gateway
