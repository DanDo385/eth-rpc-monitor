.PHONY: build clean install

# Build all binaries into bin/ directory
build:
	@mkdir -p bin
	go build -o bin/block ./cmd/block
	go build -o bin/test ./cmd/test
	go build -o bin/snapshot ./cmd/snapshot
	go build -o bin/monitor ./cmd/monitor
	@echo "Built all binaries in bin/"

# Clean all binaries
clean:
	rm -f bin/*
	@echo "Cleaned bin/ directory"

# Install builds and is an alias for build
install: build
