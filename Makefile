GO_IMAGE = golang:1.22
LINT_IMAGE = golangci/golangci-lint:v1.59.1
BUF_IMAGE = bufbuild/buf:1.45.0
NODE_IMAGE = node:22

DOCKER_NODE = docker run --rm \
	-v "$(CURDIR)":/work -w /work \
	-v uavmonitor-npmcache:/root/.npm \
	$(NODE_IMAGE)

DOCKER_WEB = docker run --rm \
	-v "$(CURDIR)/frontend":/app -w /app \
	-v uavmonitor-npmcache:/root/.npm \
	$(NODE_IMAGE)

DOCKER_GO = docker run --rm \
	-v "$(CURDIR)":/src -w /src \
	-v uavmonitor-gocache:/root/.cache/go-build \
	-v uavmonitor-gomod:/go/pkg/mod \
	-e GOFLAGS=-buildvcs=false \
	$(GO_IMAGE)

ITEST_PROJECT = uavmonitor-itest

.PHONY: build test itest vet fmt lint tidy proto up down check prettier prettier-check web-install web-lint web-test web-build

GO_PKGS = ./cmd/... ./internal/...

build:
	$(DOCKER_GO) go build $(GO_PKGS)

test:
	$(DOCKER_GO) go test -race $(GO_PKGS)

itest:
	docker compose -p $(ITEST_PROJECT) -f docker-compose.itest.yml up -d --wait
	docker run --rm --network $(ITEST_PROJECT)_default \
		-v "$(CURDIR)":/src -w /src \
		-v uavmonitor-gocache:/root/.cache/go-build \
		-v uavmonitor-gomod:/go/pkg/mod \
		-e GOFLAGS=-buildvcs=false \
		-e POSTGRES_DSN=postgres://uav:uav@postgres:5432/uav \
		-e NATS_URL=nats://nats:4222 \
		$(GO_IMAGE) go test -tags=integration -count=1 ./cmd/... ./internal/... ; \
	status=$$? ; \
	docker compose -p $(ITEST_PROJECT) -f docker-compose.itest.yml down -v ; \
	exit $$status

vet:
	$(DOCKER_GO) go vet $(GO_PKGS)

fmt:
	$(DOCKER_GO) gofmt -w cmd internal

lint:
	docker run --rm \
		-v "$(CURDIR)":/src -w /src \
		-v uavmonitor-lintcache:/root/.cache \
		-e GOFLAGS=-buildvcs=false \
		$(LINT_IMAGE) golangci-lint run

tidy:
	$(DOCKER_GO) go mod tidy

proto:
	docker run --rm -v "$(CURDIR)":/workspace -w /workspace $(BUF_IMAGE) generate

prettier:
	$(DOCKER_NODE) npx --yes prettier@3.3.3 --write .

prettier-check:
	$(DOCKER_NODE) npx --yes prettier@3.3.3 --check .

web-install:
	$(DOCKER_WEB) npm ci

web-lint:
	$(DOCKER_WEB) npx ng lint

web-test:
	$(DOCKER_WEB) sh -c "apt-get update -qq && apt-get install -y -qq chromium > /dev/null && printf '#!/bin/sh\nexec chromium --no-sandbox \"\$$@\"\n' > /usr/local/bin/chrome-ns && chmod +x /usr/local/bin/chrome-ns && CHROME_BIN=/usr/local/bin/chrome-ns npx ng test --watch=false --browsers=ChromeHeadless"

web-build:
	$(DOCKER_WEB) npx ng build

check: vet lint test prettier-check

up:
	docker compose up --build

down:
	docker compose down
