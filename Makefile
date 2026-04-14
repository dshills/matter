.PHONY: build test lint clean install help all

BINARY  := matter
CMD_PKG := ./cmd/matter

build:
	go build $(CMD_PKG)

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	go clean ./...
	rm -f $(BINARY)

install:
	go install $(CMD_PKG)

all: lint test build

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build    Build the matter binary"
	@echo "  test     Run all tests"
	@echo "  lint     Run golangci-lint"
	@echo "  clean    Remove build artifacts"
	@echo "  install  Install matter to GOPATH/bin"
	@echo "  all      Run lint, test, and build"
	@echo "  help     Show this help message"
