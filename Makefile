.PHONY: build build-all build-amd64 build-arm64 build-armv7 build-armv6
.PHONY: test lint clean run dev dev-watch tidy
.PHONY: goreleaser-check goreleaser-snapshot docker compose-up compose-down

BINARY  := afficho
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

## build: Build for the current platform
build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/afficho

## build-all: Build for all supported platforms
build-all: build-amd64 build-arm64 build-armv7 build-armv6

## build-amd64: Build for Linux x86-64
build-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 ./cmd/afficho

## build-arm64: Build for Linux ARM64 (Raspberry Pi 4+)
build-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 ./cmd/afficho

## build-armv7: Build for Linux ARMv7 (Raspberry Pi 2/3)
build-armv7:
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-armv7 ./cmd/afficho

## build-armv6: Build for Linux ARMv6 (Raspberry Pi Zero)
build-armv6:
	GOOS=linux GOARCH=arm GOARM=6 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-armv6 ./cmd/afficho

## test: Run all tests
test:
	go test -race -coverprofile=coverage.out ./...

## lint: Run linter
lint:
	golangci-lint run ./...

## tidy: Tidy go modules
tidy:
	go mod tidy

## clean: Remove build artifacts
clean:
	rm -rf bin/ dist/ coverage.out

## run: Run with production config
run: build
	./bin/$(BINARY) -config config.toml

## dev: Run with example config (no browser launch)
dev:
	go run ./cmd/afficho -config config.example.toml

## dev-watch: Run with hot-reload (rebuilds on file changes)
dev-watch:
	air

## goreleaser-check: Validate .goreleaser.yaml
goreleaser-check:
	goreleaser check

## goreleaser-snapshot: Build a local snapshot (no publish)
goreleaser-snapshot:
	goreleaser release --snapshot --clean

## docker: Build Docker image locally
docker:
	docker build -t afficho-client:dev --build-arg VERSION=$(VERSION) .

## compose-up: Start Docker Compose stack
compose-up:
	docker compose up -d --build

## compose-down: Stop Docker Compose stack
compose-down:
	docker compose down -v

## help: Show this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
