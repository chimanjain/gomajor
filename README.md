# GoMajor

A command-line tool that parses your `go.mod` file to proactively discover **major version upgrades** for your Go dependencies.

Standard Go commands (like `go list -m -u all`) often won't highlight new major version updates because Go considers different major versions (e.g., `github.com/user/gomodule/v2` vs `github.com/user/gomodule/v3`) as entirely different module paths. This tool bridges that gap by intelligently querying your Go Module Proxy (respecting `GOPROXY`) to see if a higher major version exists.

## Installation

### Using `go install`

Install the latest version of GoMajor directly:

```bash
go install github.com/chimanjain/gomajor@latest
```

Ensure your `GOBIN` or `GOPATH/bin` is in your system's `PATH`.

### Building from Source

Alternatively, clone the repository and compile the binary manually:

```bash
# Clone the repository
git clone https://github.com/chimanjain/gomajor.git
cd gomajor

# Build the executable
go build -o gomajor
```

## Usage

You can run the tool in any directory containing a `go.mod` file.

If you installed via `go install`, run:

```bash
gomajor
```

If you built from source, run the compiled binary:

```bash
./gomajor
```

### Flags

| Flag | Shorthand | Description | Default |
|------|-----------|-------------|---------|
| `--file` | `-f` | Provide a specific path to a `go.mod` file. If not provided, it will automatically search in your current working directory, and then the directory of the tool's executable. | `""` (auto-detect) |
| `--all` | `-a` | Check all dependencies, including indirect ones (marked with `// indirect` in `go.mod`). By default, only direct dependencies are analyzed. | `false` |
| `--max-probe` | `-m` | The maximum number of subsequent major versions to probe for when querying the Go proxy (e.g., if you are on `v2`, it will check up to `v7` if set to `5`). | `5` |
| `--json` | | Output the results in JSON format for easier automation and piping. | `false` |
| `--no-color` | | Disable colorized output. Useful for CI/CD logs or plain text environments. | `false` |
| `--config` | `-c` | Provide a path to a YAML configuration file to check multiple local and remote `go.mod` files. By default, checks for a `gomajor.yaml` in the current directory. | `""` (checks current directory) |
| `--output` | `-o` | Provide a path to save the structured results in YAML format. | `""` (defaults to stdout or 'gomajor-report.yaml') |

### Environment Variables

GoMajor respects the standard Go environment variables:

- `GOPROXY`: Specifies the Go module proxy to use. If not set, it defaults to `https://proxy.golang.org`.

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

---

## Multi-Source and Remote Checking (YAML Configuration)

GoMajor supports checking multiple local `go.mod` files and remote GitHub repositories concurrently using a single YAML configuration file.

### Configuration Format

Create a `gomajor.yaml` file (or any custom name) defining your local paths and remote GitHub repositories:

```yaml
# Local paths to go.mod files
local:
  - "/home/user/workspace/project1/go.mod"
  - "/home/user/workspace/project2/go.mod"

# Remote GitHub repositories or specific branches/files
github:
  - "owner/repo"                                             # Resolves main/master branch
  - "github.com/owner/repo"                                  # Shorthand style
  - "https://github.com/owner/repo"                          # Full repo link
  - "https://github.com/owner/repo/blob/develop/go.mod"      # Specific branch/file

# Output destination (optional)
output: "gomajor-report.yaml"
```

### Running with Configuration

1. **Auto-Detection**: If a `gomajor.yaml` file is present in the current working directory, simply running `./gomajor` will automatically run in multi-source mode.
2. **Explicit Config Path**: Specify a custom path to your YAML config file:

   ```bash
   ./gomajor -c my-config.yaml
   ```

3. **Explicit Output Report**: Override or set the output target file from the command line:

   ```bash
   ./gomajor -c my-config.yaml -o custom-report.yaml
   ```

### Output Formats

#### Terminal Printout (Default)

When no output file is configured, results are printed in a clean, grouped, colorized tabulation per source:

```text
/path/to/local/go.mod (local)
  ✔ All checked dependencies are on their latest major versions.

https://raw.githubusercontent.com/spf13/cobra/main/go.mod (github)
  MODULE               CURRENT   LATEST        NEW PATH
  go.yaml.in/yaml/v3   v3.0.4    v4.0.0-rc.4   go.yaml.in/yaml/v4
```

#### YAML Output Report

When an output file is specified, results are serialized into structured, valid YAML matching the schema:

```yaml
results:
  - source: /home/user/workspace/project1/go.mod
    source_type: local
    dependencies:
      - module: github.com/spf13/cobra
        current_version: v1.10.2
        latest_major_version: ""
        latest_major_path: github.com/spf13/cobra
        has_update: false
  - source: https://raw.githubusercontent.com/spf13/cobra/main/go.mod
    source_type: github
    dependencies:
      - module: go.yaml.in/yaml/v3
        current_version: v3.0.4
        latest_major_version: v4.0.0-rc.4
        latest_major_path: go.yaml.in/yaml/v4
        has_update: true
```

---

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
- **cmd**: CLI interface built with Cobra. Decoupled into modular cohesive components:
  - `types.go`: Defines configurations and output formats.
  - `github.io`: Resolves and normalizes remote candidates.
  - `runner.go`: Unifies concurrent checker flows.
  - `formatter.go`: Handles visual printouts.
  - `root.go`: Bootstraps CLI bindings.
