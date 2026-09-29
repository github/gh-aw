# testutil Package

> Shared test helpers for isolated temporary directories, stderr capture, and YAML comment stripping in Go test files.

The `testutil` package provides shared test helpers for isolating test artifacts and capturing output.

## Overview

This package is imported only in test files (`_test.go`). It provides:
- A shared, isolated temporary directory for each test run (outside the git repository).
- Per-test subdirectories that are cleaned up automatically.
- Helpers for capturing `os.Stderr` output during tests.
- A helper for stripping YAML comment headers from compiled workflow output.
- A helper for locating the built `gh-aw` binary for integration tests.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `GetTestRunDir` | `func() string` | Returns the path to the unique top-level directory for the current test run, created once per process under `$TMPDIR/gh-aw-test-runs/<timestamp>-<pid>` |
| `TempDir` | `func(t *testing.T, pattern string) string` | Creates a temporary subdirectory inside the test run directory matching `pattern`; the directory is automatically removed when the test completes via `t.Cleanup` |
| `CaptureStderr` | `func(t *testing.T, fn func()) string` | Runs `fn` and returns everything written to `os.Stderr` during its execution; `os.Stderr` is restored via `defer` when `fn` returns |
| `RequireGhAwBinary` | `func(t *testing.T) string` | Returns the absolute path to the built `gh-aw` binary (`gh-aw.exe` on Windows) at the repository root, found by walking up from the working directory to `go.mod`; the test is skipped via `t.Skip` if the root or binary is missing (run `make build` first) |
| `StripYAMLCommentHeader` | `func(yamlContent string) string` | Removes the leading comment block from a generated YAML file and returns only the non-comment content |

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/testutil"

func TestMyFunction(t *testing.T) {
    // Create an isolated temp directory for test artifacts
    dir := testutil.TempDir(t, "my-test-*")
    // dir is cleaned up automatically when the test ends

    // Capture output written to os.Stderr
    output := testutil.CaptureStderr(t, func() {
        myFunction() // function that writes to os.Stderr
    })
    assert.Contains(t, output, "expected message")
}
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/constants` — shared filesystem permission constants for temp directory creation

## Design Decisions

- `GetTestRunDir` uses `sync.Once` so the directory is created exactly once per process even when multiple test packages run concurrently.
- `TempDir` delegates to `os.MkdirTemp` to generate unique subdirectory names.
- Test artifacts placed in the test run directory are outside any git repository, which prevents `git` commands executed by tests from picking them up as untracked files.

## Thread Safety

`GetTestRunDir` is safe to call concurrently from multiple goroutines; the underlying `sync.Once` ensures the directory is created exactly once. `TempDir` and `CaptureStderr` require a valid `*testing.T` and MUST be called from a single test goroutine only (as required by the `testing` package).

<!-- BEGIN SOURCE-VERIFIED EXPORT COVERAGE -->
## Source-verified export coverage

This appendix is generated from the current non-test Go source files in this package and records any exported top-level symbols that are not already described above.

| Category | Count |
|----------|------:|
| Types | 0 |
| Constants | 0 |
| Variables | 0 |
| Functions and methods | 5 |
| Additional symbols documented in this appendix | 0 |

The sections above already mention every exported top-level symbol in the current source tree.
<!-- END SOURCE-VERIFIED EXPORT COVERAGE -->

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
