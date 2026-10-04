.PHONY: help lint test check build docker run up up-dev down logs contract-update

CONTRACT_PATH ?= ../sm_smart_home_core_go/contract

help:
	@echo "Available targets:"
	@echo "  help            - Show this help message"
	@echo "  lint            - Run golangci-lint"
	@echo "  test            - Run unit tests and architecture checks with -race"
	@echo "  check           - Run lint and test"
	@echo "  build           - Compile tg-gateway binary into bin/"
	@echo "  docker          - Build Docker image for tg-gateway"
	@echo "  run             - Run tg-gateway locally with .env"
	@echo "  up              - Build and start tg-gateway via docker-compose (homelab mode)"
	@echo "  up-dev          - Build and start tg-gateway via docker-compose.dev.yml (desktop dev mode)"
	@echo "  down            - Stop services via docker-compose"
	@echo "  logs            - Tail service logs via docker-compose"
	@echo "  contract-update - Update contract module (usage: make contract-update VERSION=vX.Y.Z)"

lint:
	go tool golangci-lint run

test:
	go test -race ./...

check: lint test

build:
	mkdir -p bin
	go build -o bin/tg-gateway ./cmd/tg-gateway

docker:
	docker build --build-context contract=$(CONTRACT_PATH) -t simple-smart-home-tg-gateway:latest .

run: build
	./bin/tg-gateway

up:
	docker compose up -d --build

up-dev:
	docker compose -f docker-compose.dev.yml up -d --build

down:
	docker compose -f docker-compose.yml -f docker-compose.dev.yml down

logs:
	docker compose logs -f

contract-update:
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION is required (e.g. make contract-update VERSION=v0.1.0)"; \
		exit 1; \
	fi
	go get github.com/Alex84K/core_syst_go/contract@$(VERSION)
	go mod tidy
