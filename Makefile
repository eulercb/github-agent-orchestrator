.PHONY: build install test lint fmt clean

BINARY_NAME := gao
BUILD_DIR := ./build
MAIN_PKG := ./cmd/gao

GO := go
GOFLAGS := -trimpath
LDFLAGS := -s -w

build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PKG)

install:
	$(GO) install $(GOFLAGS) -ldflags "$(LDFLAGS)" $(MAIN_PKG)

test:
	$(GO) test -race -count=1 ./...

test-cover:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	goimports -w .

clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

all: lint test build
