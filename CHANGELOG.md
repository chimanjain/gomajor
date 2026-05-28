# Changelog

All notable changes to **GoMajor** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
