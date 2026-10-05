VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DEV_CONFIG ?= shed.dev.toml

.PHONY: web build site test dev

web:
	cd web && bun ci && bun run build

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/shed ./cmd/shed

site:
	cd web && bun ci && bun run site:build

test:
	go test -race ./...

dev:
	go run ./cmd/shed -config $(DEV_CONFIG)
