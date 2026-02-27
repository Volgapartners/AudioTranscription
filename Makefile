.PHONY: build run clean test tidy vet fmt lint build-windows build-linux build-all

# Build the CLI binary (current OS/arch)
build:
	go build -o bin/transcribe ./cmd/transcribe

# Cross-platform builds
build-windows:
	GOOS=windows GOARCH=amd64 go build -o bin/transcribe-windows-amd64.exe ./cmd/transcribe

build-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/transcribe-linux-amd64 ./cmd/transcribe

build-all: build build-windows build-linux

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
