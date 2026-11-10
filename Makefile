.PHONY: all build test coverage clean

ifeq ($(OS),Windows_NT)
BINARY_NAME ?= gomajor.exe
# On Windows, -race requires MinGW-w64 gcc and CGO. Omit by default unless overridden.
RACE_FLAG ?=
else
BINARY_NAME ?= gomajor
RACE_FLAG ?= -race
endif

all: build test

build:
	@echo "==> Building gomajor..."
	go build -o $(BINARY_NAME) ./cmd/gomajor

test:
	@echo "==> Running tests..."
	go test -v $(RACE_FLAG) ./...

coverage:
	@echo "==> Generating test coverage report..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "==> Coverage report generated at coverage.html"

clean:
	@echo "==> Cleaning up..."
	go clean
ifeq ($(OS),Windows_NT)
	-@del /f /q $(BINARY_NAME) 2>nul || rm -f $(BINARY_NAME) 2>/dev/null || true
	-@del /f /q coverage.out coverage.html 2>nul || rm -f coverage.out coverage.html 2>/dev/null || true
else
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
endif
