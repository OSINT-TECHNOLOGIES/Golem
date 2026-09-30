.PHONY: build run test clean install release

BINARY := golem
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	@echo "Building $(BINARY) $(VERSION)..."
	@go build -ldflags "-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/golem

install: build
	@echo "Installing to /usr/local/bin..."
	@sudo mv $(BINARY) /usr/local/bin/

run: build
	@./$(BINARY) $(ARGS)

test:
	@go test -v ./...

clean:
	@rm -f $(BINARY)
	@rm -rf report/ dist/

deps:
	@go mod tidy
	@go mod download

lint:
	@go vet ./...
	@gofmt -l .

release:
	@goreleaser release --clean

release-snapshot:
	@goreleaser release --snapshot --clean

docker:
	@docker build -t $(BINARY):$(VERSION) .

scan-gpt:
	@./$(BINARY) scan gpt-4o-mini --quick

scan-claude:
	@./$(BINARY) scan claude-3-5-sonnet-20241022 --quick

scan-local:
	@./$(BINARY) scan llama3.1 --quick