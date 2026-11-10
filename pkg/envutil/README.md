# envutil Package

> Reads and validates environment variables with consistent default handling.

## Overview

The `envutil` package centralizes the pattern of reading environment variables, validating typed values where needed, and falling back to a default value when the variable is absent or invalid. Warnings for invalid typed values are formatted to stderr when no logger is supplied; when a logger is supplied, they are routed through that logger.

The package exposes helpers for integer, boolean, and string values so callers can avoid scattered `os.Getenv` checks while preserving consistent defaulting and debug logging behavior.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `GetIntFromEnv` | `func(envVar string, defaultValue, minValue, maxValue int, debugLog *logger.Logger) int` | Reads an integer-valued environment variable, validates it, and returns a default when absent or invalid |
| `GetBoolFromEnv` | `func(envVar string, defaultValue bool, debugLog *logger.Logger) bool` | Reads a boolean-valued environment variable and returns a default when absent or invalid |
| `GetStringFromEnv` | `func(envVar, defaultValue string, debugLog *logger.Logger) string` | Reads a string-valued environment variable and returns a default when absent |

#### `GetIntFromEnv`

```go
func GetIntFromEnv(envVar string, defaultValue, minValue, maxValue int, debugLog *logger.Logger) int
```

Reads `envVar` from the process environment, parses it as an integer, validates it against `[minValue, maxValue]`, and returns `defaultValue` when the variable is absent, unparseable, or out of range.

| Parameter | Type | Description |
|-----------|------|-------------|
| `envVar` | `string` | Environment variable name (e.g. `"GH_AW_TIMEOUT"`) |
| `defaultValue` | `int` | Value returned when env var is absent or invalid |
| `minValue` | `int` | Minimum allowed value (inclusive) |
| `maxValue` | `int` | Maximum allowed value (inclusive) |
| `debugLog` | `*logger.Logger` | Optional logger for debug output; pass `nil` to disable |

**Behavioral contract**:
- MUST return `defaultValue` when the environment variable is not set (empty string).
- MUST return `defaultValue` and emit a warning when the value cannot be parsed as an integer.
- MUST return `defaultValue` and emit a warning when the value is outside `[minValue, maxValue]` (bounds are inclusive).
- MUST log the accepted value via `debugLog` when `debugLog` is non-nil.
- MUST emit warnings formatted via `console.FormatWarningMessage` to `os.Stderr` when `debugLog` is `nil`.
- MUST route warnings through `debugLog.Printf` when `debugLog` is non-nil; the logger controls whether and how the message is emitted.

#### `GetBoolFromEnv`

```go
func GetBoolFromEnv(envVar string, defaultValue bool, debugLog *logger.Logger) bool
```

Reads `envVar` from the process environment, parses it as a boolean using Go's standard `strconv.ParseBool` rules, and returns `defaultValue` when the variable is absent or unparseable.

| Parameter | Type | Description |
|-----------|------|-------------|
| `envVar` | `string` | Environment variable name (e.g. `"CI"`) |
| `defaultValue` | `bool` | Value returned when env var is absent or invalid |
| `debugLog` | `*logger.Logger` | Optional logger for debug output; pass `nil` to disable |

**Behavioral contract**:
- MUST return `defaultValue` when the environment variable is not set (empty string).
- MUST return `defaultValue` and emit a warning when the value cannot be parsed as a boolean.
- MUST log the accepted value via `debugLog` when `debugLog` is non-nil.
- MUST emit warnings formatted via `console.FormatWarningMessage` to `os.Stderr` when `debugLog` is `nil`.
- MUST route warnings through `debugLog.Printf` when `debugLog` is non-nil; the logger controls whether and how the message is emitted.

#### `GetStringFromEnv`

```go
func GetStringFromEnv(envVar, defaultValue string, debugLog *logger.Logger) string
```

Reads `envVar` from the process environment and returns `defaultValue` when the variable is absent or empty.

| Parameter | Type | Description |
|-----------|------|-------------|
| `envVar` | `string` | Environment variable name (e.g. `"GITHUB_TOKEN"`) |
| `defaultValue` | `string` | Value returned when env var is absent or empty |
| `debugLog` | `*logger.Logger` | Optional logger for debug output; pass `nil` to disable |

**Behavioral contract**:
- MUST return `defaultValue` when the environment variable is not set (empty string).
- MUST return the environment variable value unchanged when it is non-empty.
- SHOULD log only that the variable was found, without logging its value, via `debugLog` when `debugLog` is non-nil.

## Usage Examples

```go
import (
    "github.com/github/gh-aw/pkg/envutil"
    "github.com/github/gh-aw/pkg/logger"
)

var log = logger.New("mypackage:config")

// Read GH_AW_MAX_CONCURRENT_DOWNLOADS, constrained to [1, 20], default 5
concurrency := envutil.GetIntFromEnv("GH_AW_MAX_CONCURRENT_DOWNLOADS", 5, 1, 20, log)

// Read CI, defaulting to false when unset or invalid
isCI := envutil.GetBoolFromEnv("CI", false, log)

// Read GITHUB_TOKEN, defaulting to empty string when unset
token := envutil.GetStringFromEnv("GITHUB_TOKEN", "", log)

// Suppress debug output by passing nil logger
timeout := envutil.GetIntFromEnv("GH_AW_TIMEOUT", 60, 1, 3600, nil)
```

## Thread Safety

`GetIntFromEnv`, `GetBoolFromEnv`, and `GetStringFromEnv` are safe for concurrent use. They hold no shared mutable state; each invocation reads the process environment via `os.Getenv` and operates on function-local variables only.

## Design Decisions

- Warning messages use `console.FormatWarningMessage` so they render consistently in terminals.
- Warnings go directly to `os.Stderr` only when no logger is supplied; otherwise they are passed to the logger, which controls their output.
- Typed helpers use Go standard library parsing rules (`strconv.Atoi`, `strconv.ParseBool`) for predictable behavior.
- `GetStringFromEnv` logs a message of the form `Using ENV_VAR from environment` when a value is found, never the value itself, so secret-like values can be read without echoing their contents.
- When `debugLog` is non-nil, warnings are routed through the logger rather than written directly to stderr, allowing callers to control output formatting.

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/console` — warning message formatting
- `github.com/github/gh-aw/pkg/logger` — debug logging

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
