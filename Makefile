.PHONY: all build test coverage clean

BINARY_NAME=gomajor

all: build test

build:
	@echo "==> Building gomajor..."
	go build -o $(BINARY_NAME) .

test:
	@echo "==> Running tests..."
	go test -v -race ./...

coverage:
	@echo "==> Generating test coverage report..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "==> Coverage report generated at coverage.html"

clean:
	@echo "==> Cleaning up..."
	go clean
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
