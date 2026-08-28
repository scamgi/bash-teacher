BINARY  := bt
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build run test lint fmt vet check clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/bt

run: build
	./$(BINARY)

test:
	go test ./...

# lint validates the content library the same way CI does.
lint:
	go run ./cmd/bt content lint

fmt:
	gofmt -l -w .

vet:
	go vet ./...

check: vet test lint

clean:
	rm -f $(BINARY)
