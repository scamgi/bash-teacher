BINARY  := bt
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build run test lint lint-go expected fmt vet check clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/bt

run: build
	./$(BINARY)

test:
	go test ./...

# lint validates the content library the same way CI does.
lint:
	go run ./cmd/bt content lint

# expected regenerates every exercise's expected output by running its reference
# solution in the sandbox. Run it after editing an exercise or a fixture; the
# reference-solution test in internal/runner is what checks it stayed in sync.
expected:
	go run ./cmd/bt content expected --write

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
