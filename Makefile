BINARY_DIR := bin
BINARY := $(BINARY_DIR)/code-context

.PHONY: build test fmt tidy clean

build:
	mkdir -p $(BINARY_DIR)
	go build -o $(BINARY) ./cmd/code-context

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

tidy:
	go mod tidy

clean:
	rm -rf $(BINARY_DIR)
