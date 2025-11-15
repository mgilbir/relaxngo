.PHONY: test test-official test-all test-generator test-benchmarks build clean lint spec-test-data bench-gen bench help

# Default target
all: test-all

# Run Go test suite
test:
	@echo "Running Go test suite..."
	go test -count=1 -race ./...

# Run generator-specific tests
test-generator:
	@echo "Running generator tests..."
	go test ./generator -v

# Run official RELAX NG test suite
test-official:
	@echo "Running official test suite..."
	go run cmd/internal/official-tests/main.go

# Run all tests (Go tests + official specs)
test-all: test test-official test-generator
	@echo "All tests completed!"

# Generate benchmark projects
bench-gen:
	@echo "Generating benchmark projects..."
	go run ./cmd/internal/generate-benchmark-projects -output /tmp/benchmark_projects

# Run clean benchmarks
bench: bench-gen
	@echo "Running clean benchmarks..."
	go test ./benchmarks -run '^$$' -bench 'BenchmarkGeneratorClean' -count=1 -v -args -benchmark-projects-dir=/tmp/benchmark_projects

# Build the project
build:
	@echo "Building..."
	go build ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	go clean

# Run linter
lint:
	@echo "Running golangci-lint..."
	golangci-lint run

spec-test-data:
	@echo "Building the container..."
	cd spec/tests && docker build -t relaxng-split .
	@echo "Running the spec test data generation..."
	mkdir -p testdata/official-tests-2
	docker run --rm --user $(shell id -u):$(shell id -g) -v $(PWD)/testdata/official-tests-2:/out relaxng-split && \
	rm -rf testdata/official-tests && \
	mv testdata/official-tests-2 testdata/official-tests

# Show help
help:
	@echo "Available targets:"
	@echo "  make test            - Run Go test suite"
	@echo "  make test-generator  - Run generator package tests"
	@echo "  make test-official   - Run official RELAX NG test suite"
	@echo "  make test-all        - Run all tests (default)"
	@echo "  make bench-gen       - Generate benchmark projects"
	@echo "  make bench           - Run clean benchmarks (includes generation)"
	@echo "  make build           - Build the project"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make lint            - Run golangci-lint"
	@echo "  make help            - Show this help message"
