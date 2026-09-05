.PHONY: all build run test lint load-test docker-up docker-down clean

# default target
all: build

# build gateway binary
build:
	go build -o gateway ./cmd/gateway

# run gateway server locally
run:
	go run ./cmd/gateway/main.go

# run unit and race tests
test:
	go test -v -race -coverprofile=coverage.out ./...

# run golangci-lint
lint:
	golangci-lint run ./...

# run k6 load testing script
load-test:
	k6 run scripts/load_test.js

# spin up docker compose stack
docker-up:
	docker-compose up -d --build

# teardown docker compose stack
docker-down:
	docker-compose down -v

# clean compiled binaries and artifacts
clean:
	rm -f gateway coverage.out
