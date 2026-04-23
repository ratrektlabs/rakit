.PHONY: all build test test-race lint vet fmt tidy clean run-local help

GO ?= go

all: vet test build

build:
	$(GO) build ./...

test:
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

lint:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not installed: see https://golangci-lint.run/usage/install/"; exit 1; \
	fi
	golangci-lint run ./...

tidy:
	$(GO) mod tidy

clean:
	$(GO) clean ./...
	rm -rf examples/local/data

run-local:
	cd examples/local && $(GO) run .

help:
	@echo "make build      - compile all packages"
	@echo "make test       - run unit tests"
	@echo "make test-race  - run tests with the race detector"
	@echo "make vet        - run go vet"
	@echo "make lint       - run golangci-lint"
	@echo "make fmt        - gofmt -w"
	@echo "make tidy       - go mod tidy"
	@echo "make run-local  - run the examples/local server"
