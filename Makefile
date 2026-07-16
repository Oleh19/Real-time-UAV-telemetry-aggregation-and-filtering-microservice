GO_IMAGE = golang:1.22
LINT_IMAGE = golangci/golangci-lint:v1.59.1
BUF_IMAGE = bufbuild/buf:1.45.0

DOCKER_GO = docker run --rm \
	-v "$(CURDIR)":/src -w /src \
	-v uavmonitor-gocache:/root/.cache/go-build \
	-v uavmonitor-gomod:/go/pkg/mod \
	-e GOFLAGS=-buildvcs=false \
	$(GO_IMAGE)

.PHONY: build test vet fmt lint tidy proto up down check

build:
	$(DOCKER_GO) go build ./...

test:
	$(DOCKER_GO) go test -race ./...

vet:
	$(DOCKER_GO) go vet ./...

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

check: vet lint test

up:
	docker compose up --build

down:
	docker compose down
