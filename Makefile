BINARY_SPACECRAFT   := spacecraft
BINARY_GROUNDSTATION := groundstation
SERVICES            := $(BINARY_SPACECRAFT) $(BINARY_GROUNDSTATION)

GO          := go
GOFLAGS     := -race
CGO_ENABLED := 0

.PHONY: all build test test-unit lint vet tidy docker-up docker-down clean

all: build

## Build both service binaries.
build: build-spacecraft build-groundstation

build-spacecraft:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -ldflags="-s -w" -o build/$(BINARY_SPACECRAFT) ./cmd/spacecraft

build-groundstation:
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -ldflags="-s -w" -o build/$(BINARY_GROUNDSTATION) ./cmd/groundstation

## Run all unit tests with the race detector and coverage.
test:
	$(GO) test $(GOFLAGS) -count=1 -cover ./...

## Short tests only (skips integration and slow orbital tests).
test-unit:
	$(GO) test $(GOFLAGS) -count=1 -short ./pkg/... ./spacecraft/... ./groundstation/...

## Run integration tests (requires no live services).
test-integration:
	$(GO) test $(GOFLAGS) -count=1 -run Integration ./integration/...

## Vet all packages.
vet:
	$(GO) vet ./...

## Tidy go.mod and go.sum.
tidy:
	$(GO) mod tidy

## Start all services via Docker Compose.
docker-up:
	docker compose -f docker/docker-compose.yaml up --build

## Stop services and remove containers.
docker-down:
	docker compose -f docker/docker-compose.yaml down

## Build the C protocol library.
c-build:
	$(MAKE) -C c

## Run C library tests.
c-test:
	$(MAKE) -C c test

## Remove build artifacts.
clean:
	rm -rf build/

## Print help.
help:
	@grep -h "##" $(MAKEFILE_LIST) | grep -v grep | sed -e 's/##//'
