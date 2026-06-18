# Changelog

All notable changes to **GoMajor** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.5.0] - 2026-06-18

### Added

- **Private Modules Support**: Introduced automatic identification for private modules matching `GOPRIVATE` and `GONOPROXY` environment variables, skipping external proxy queries to avoid timeouts.
- **GitHub Actions Integration**: Added a reusable dependency audit workflow (`.github/workflows/gomajor-dependency-audit.yml`) and documentation for automated periodic module checking in CI/CD.
- **Sensitive URL Sanitization**: Added URL credentials sanitization to redact passwords/tokens from git and remote repository URLs in reports and terminal output.

### Changed

- **Improved Module Path Parsing**: Enhanced parsing logic to extract and match path separators (`/` vs `.`) to format subsequent major version candidate lookup paths.
- **HTTP Client Optimization**: Configured custom HTTP transport settings to increase maximum idle connections and idle connections per host, improving connection reuse during high-concurrency proxy checks.
- **Concurrency & Resource Management**: Implemented connection request throttling using semaphore-based concurrency limits and singleflight coalescing to coalesce identical concurrent module requests and prevent proxy rate limiting.
- **Context Propagation**: Added full context propagation throughout the check and run operations to support clean cancellation and timeout handling.

---

## [1.4.0] - 2026-06-01

### Added

- **Minor Version Update Detection**: Added the `--minor` and `--major` command-line flags (both defaulting to `true`) to allow toggling checks for minor version updates within the current major version, and major version upgrades respectively. Added corresponding `minor` and `major` configuration fields within YAML config profiles.
- **JSON Report and Output Support**: Added support for saving scan reports in JSON format via the `--output` / `-o` option (automatically detected from a `.json` file extension, otherwise defaulting to YAML). Added the `--json` flag to print structured scan results to stdout, and added an `examples/gomajor-json.yaml` configuration profile template.
- **Comprehensive Integration Tests**: Replaced old test suites with extensive, mock-friendly unit and integration tests under `cmd/runner_test.go` that validate multiple sources, custom GOPROXY clients, and JSON/YAML reporting.

---

## [1.3.0] - 2026-05-28

### Added

- **Direct GitHub repository checking**: Introduced `--github` / `-g` command-line flags to check remote GitHub repositories directly from the CLI as a comma-separated list, bypassing the need for a YAML configuration file.
- **In-memory proxy lookup caching**: Implemented thread-safe caching within `checker.Client` for Go Module Proxy queries. This bypasses redundant lookup requests for identical module paths in multi-source scans, accelerating execution times and minimizing remote network calls.
- **Extensive test coverage**: Added rigorous unit tests in `cmd/runner_test.go` and `checker/checker_test.go` verifying proxy cache hits, remote GitHub direct inputs, and concurrent lookups.

---

## [1.2.0] - 2026-05-25

### Added

- **Multi-Source Configuration scanning**: Enabled scanning multiple local `go.mod` files and remote GitHub repositories concurrently via a central YAML configuration file (`gomajor.yaml`).
- **Remote GitHub checking**: Added support to fetch `go.mod` files directly using the GitHub API. It supports standard repository shorthands (`owner/repo`), full GitHub URLs, or deep links targeting specific files or branches.
- **YAML reports**: Introduced `--output` / `-o` command-line flags and a matching YAML schema to serialize structured scanning results to a file (defaults to `gomajor-report.yaml`).
- **Configuration Auto-Detection**: Automatically searches for and runs under multi-source mode if a `gomajor.yaml` file exists in the current working directory.
- **Subcommand support**: Added the `github` subcommand as an alternative way to check a remote repository directly from the command line.
- **Example YAML profiles**: Added configurations illustrating different scenarios:
  - `examples/gomajor.local.yaml` for scanning local workspaces.
  - `examples/gomajor.github.yaml` for scanning remote GitHub repositories.
  - `examples/gomajor.full.yaml` showcasing all YAML schema properties (source paths, custom proxy settings, and output files).

---

## [1.1.0] - 2026-05-12

### Added

- **JSON output support**: Added the `--json` command-line flag to output findings in machine-readable JSON format, enabling seamless integration into automated scripting and CI/CD pipelines.
- **No-color mode**: Added the `--no-color` command-line flag to suppress terminal ANSI escape color sequences, useful for plain-text logs or non-interactive environments.
- **GOPROXY environment support**: Integrated respect for the standard `GOPROXY` environment variable. Gomajor now routes all dependency lookup probes through custom, internal, or commercial Go proxies rather than defaulting exclusively to the public Go Proxy.
- **Robust test additions**:
  - `cmd/root_test.go`: Added test cases for standard argument bindings and `--json` format serialization.
  - `checker/checker_test.go`: Added assertions to verify GOPROXY endpoint URL parsing and fallback behaviors.

---

## [1.0.0] - 2026-04-30

### Added

- **Initial Stable Release**:
  - Proactive **major version upgrade detection** (e.g. from `/v2` to `/v3`) which traditional tools overlook due to distinct module path schemas.
  - Multi-prober lookup algorithm queried directly against Go module proxy endpoints.
  - Customized interactive depth checks limited via the `--max-probe` / `-m` parameter.
  - Auto-scans of local directories with `--file` / `-f` specification overrides.
  - Indirect dependency inspections enabled with the `--all` / `-a` flag.
  - Colorful and informative CLI report interface displaying module names, current tag versions, and newly discovered pathways.

---
