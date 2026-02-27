.PHONY: build run clean test tidy vet fmt lint

# Build the CLI binary
build:
	go build -o bin/transcribe ./cmd/transcribe

# Run the CLI
run: build
	./bin/transcribe

# Run with verbose logging
run-verbose: build
	./bin/transcribe --verbose

# Clean build artifacts
clean:
	rm -rf bin/

# Run all tests
test:
	go test -v -race ./...

# Tidy dependencies
tidy:
	go mod tidy

# Vet code
vet:
	go vet ./...

# Format code
fmt:
	gofmt -s -w .

# Lint (requires golangci-lint)
lint:
	golangci-lint run ./...

# Full check
check: fmt vet test
