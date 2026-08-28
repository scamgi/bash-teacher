BINARY  := bt
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build run test lint lint-go fmt vet check clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/bt

run: build
	./$(BINARY)

test:
	go test ./...

# lint validates the content library the same way CI does.
lint:
	go run ./cmd/bt content lint

# lint-go runs golangci-lint with the config in .golangci.yml.
lint-go:
	golangci-lint run

fmt:
	gofmt -l -w .

vet:
	go vet ./...

check: vet lint-go test lint

clean:
	rm -f $(BINARY)
