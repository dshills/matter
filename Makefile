.PHONY: build build-server test lint clean install help all

BINARY        := matter
SERVER_BINARY := matter-server
CMD_PKG       := ./cmd/matter
SERVER_PKG    := ./cmd/matter-server

build:
	go build $(CMD_PKG)

build-server:
	go build $(SERVER_PKG)

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	go clean ./...
	rm -f $(BINARY) $(SERVER_BINARY)

install:
	go install $(CMD_PKG)
	go install $(SERVER_PKG)

all: lint test build build-server

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build         Build the matter CLI binary"
	@echo "  build-server  Build the matter-server binary"
	@echo "  test          Run all tests"
	@echo "  lint          Run golangci-lint"
	@echo "  clean         Remove build artifacts"
	@echo "  install       Install matter and matter-server to GOPATH/bin"
	@echo "  all           Run lint, test, build, and build-server"
	@echo "  help          Show this help message"
