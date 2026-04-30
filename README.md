# GoMajor

A command-line tool that parses your `go.mod` file to proactively discover **major version upgrades** for your Go dependencies.

Standard Go commands (like `go list -m -u all`) often won't highlight new major version updates because Go considers different major versions (e.g., `github.com/user/gomodule/v2` vs `github.com/user/gomodule/v3`) as entirely different module paths. This tool bridges that gap by intelligently querying the Go Module Proxy (`proxy.golang.org`) to see if a higher major version exists.

## How to Build

Clone this repository and compile the binary using `go build`:

```bash
# Clone the repository
git clone https://github.com/chimanjain/gomajor.git
cd gomajor

# Build the executable
go build -o gomajor
```

## Usage

You can run the tool in any directory containing a `go.mod` file:

```bash
./gomajor
```

### Flags

| Flag | Shorthand | Description | Default |
|------|-----------|-------------|---------|
| `--file` | `-f` | Provide a specific path to a `go.mod` file. If not provided, it will automatically search in your current working directory, and then the directory of the tool's executable. | `""` (auto-detect) |
| `--all` | `-a` | Check all dependencies, including indirect ones (marked with `// indirect` in `go.mod`). By default, only direct dependencies are analyzed. | `false` |
| `--max-probe` | `-m` | The maximum number of subsequent major versions to probe for when querying the Go proxy (e.g., if you are on `v2`, it will check up to `v7` if set to `5`). | `5` |

## Examples

**Check direct dependencies in the current directory:**
```bash
./gomajor
```

**Check all dependencies (direct and indirect) for a specific project:**
```bash
./gomajor --file /path/to/your/project/go.mod --all
```

**Probe further into the future (check up to 10 major versions ahead):**
```bash
./gomajor -m 10
```

## Development

### Running Tests

The project includes comprehensive unit tests for both the checker and cmd packages:

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests for a specific package
go test ./checker
go test ./cmd
```

### Architecture

- **checker**: Core logic for detecting major version updates by querying the Go Module Proxy. The `Client` struct encapsulates HTTP operations and can be configured with custom HTTP clients and proxy URLs.
- **cmd**: CLI interface built with Cobra. Uses a `Config` struct for dependency injection, making it easy to test without relying on global state.
