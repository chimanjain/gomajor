# GoMajor

<p align="center">
  <img src="assets/gomajor.jpeg" alt="GoMajor Logo" width="200" height="200" />
</p>

A sleek, color-coded CLI tool that parses `go.mod` files to proactively discover **minor updates** (🟢 Green) and **major upgrades** (🟡 Yellow) by querying your Go Module Proxy (respecting `GOPROXY`).

Standard commands like `go list -m -u all` fail to highlight new major versions because Go treats different major versions (e.g. `/v2` vs `/v3`) as separate modules. **GoMajor** bridges this gap.

---

## Quick Start

### Installation

```bash
# Install instantly
go install github.com/chimanjain/gomajor@latest

# Or build from source
git clone https://github.com/chimanjain/gomajor.git && cd gomajor && go build -o gomajor
```

### Basic CLI Usage

```bash
# Check direct dependencies in the current directory
gomajor

# Check all (including indirect) dependencies for a specific file
gomajor -f /path/to/go.mod --all

# Disable minor or major checks explicitly
gomajor --minor=false
gomajor --major=false

# Check remote GitHub repositories directly
gomajor -g owner/repo,github.com/owner/repo2
```

---

## Configuration & Flags

### CLI Flags

| Flag | Shorthand | Description | Default | Example |
|------|-----------|-------------|---------|---------|
| `--file` | `-f` | Path to target `go.mod` file | `""` (auto-detect) | `gomajor -f ./sub/go.mod` |
| `--all` | `-a` | Check indirect dependencies too | `false` | `gomajor -a` |
| `--max-probe` | `-m` | Max subsequent major versions to probe | `5` | `gomajor -m 10` |
| `--minor` | | Toggle minor version updates checking | `true` | `gomajor --minor=false` |
| `--major` | | Toggle major version upgrades checking | `true` | `gomajor --major=false` |
| `--config` | `-c` | Path to multi-source YAML config file | `"gomajor.yaml"` | `gomajor -c my-config.yaml` |
| `--github` | `-g` | Direct comma-separated GitHub repositories | `""` | `gomajor -g owner/repo` |
| `--output` | `-o` | Save results to a structured YAML report file | `""` | `gomajor -o report.yaml` |
| `--json` | | Output raw JSON data directly | `false` | `gomajor --json` |
| `--no-color` | | Suppress ANSI colored terminal formatting | `false` | `gomajor --no-color` |

### Multi-Source Checking (`gomajor.yaml`)

Define multiple local directories and remote GitHub repositories to analyze in a single run:

```yaml
local:
  - "/path/to/project1/go.mod"
github:
  - "owner/repo"
  - "https://github.com/owner/repo2/blob/develop/go.mod"
output: "gomajor-report.yaml"
minor: true
major: true
```

### Environment Variables

GoMajor respects the standard Go module proxy env:

- `GOPROXY`: Specifies the Go module proxy URL (defaults to `https://proxy.golang.org`).

---

## Output Examples

### Terminal Format (Default)

```text
https://raw.githubusercontent.com/spf13/cobra/main/go.mod (github)
  MODULE               CURRENT   MINOR     MAJOR         NEW PATH
  go.yaml.in/yaml/v3   v3.0.4    -         v4.0.0-rc.4   go.yaml.in/yaml/v4
```

### YAML Report Format

```yaml
results:
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

## Development & Architecture

### Testing

```bash
go test -cover ./...
```

### Architecture

- **`checker`**: Core engine for querying the Go Module Proxy.
- **`utils`**: Centralized, zero-dependency package for Go module path parsing, version path formatting, and proxy path escaping.
- **`cmd`**: Decoupled CLI architecture built with Cobra:
  - `runner.go` / `runner_single.go` / `runner_multi.go`: Execution flow routing and concurrent proxy processing.
  - `formatter.go` / `types.go`: Output visualization structures.
  - `github.go` / `root.go`: Remote path parsing and Cobra commands bootstrapping.
