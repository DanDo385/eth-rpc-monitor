.PHONY: build clean install test vet

# Build the single ethrpc CLI binary into bin/
build:
	@mkdir -p bin
	go build -o bin/ethrpc ./cmd/ethrpc
	@echo "Built bin/ethrpc"

# Clean all binaries
clean:
	rm -f bin/*
	@echo "Cleaned bin/ directory"

# Install builds and is an alias for build
install: build

test:
	go test ./... -race -count=1

vet:
	go vet ./...
