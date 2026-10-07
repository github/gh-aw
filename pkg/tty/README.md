# tty Package

> TTY (terminal) detection utilities for standard input, output, and error streams.

## Overview

This package exposes functions for checking whether standard input, output, or error streams are connected to a real terminal. The detection uses `golang.org/x/term`, which is the same library used by the spinner and progress-bar components in this codebase.

On WebAssembly targets (`js/wasm`) the package provides stub implementations that always return `false`, since WASM environments do not have real TTY file descriptors.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `IsStdinTerminal` | `func() bool` | Returns `true` if `stdin` (`os.Stdin`) is connected to a terminal |
| `IsStdoutTerminal` | `func() bool` | Returns `true` if `stdout` (`os.Stdout`) is connected to a terminal |
| `IsStderrTerminal` | `func() bool` | Returns `true` if `stderr` (`os.Stderr`) is connected to a terminal |

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/tty"

if tty.IsStdinTerminal() {
    // Safe to request interactive input from stdin
}

if tty.IsStdoutTerminal() {
    // Safe to emit colored or animated output to stdout
    fmt.Println(coloredOutput)
} else {
    // Plain output for pipes/redirects
    fmt.Println(plainOutput)
}

if tty.IsStderrTerminal() {
    // Safe to emit spinner or progress animation to stderr
}
```

## Dependencies

**Internal**:
- None

**External**:
- `golang.org/x/term` — TTY detection via `term.IsTerminal`

## Design Notes

- Terminal detection is evaluated at call time, not cached. This is intentional: the streams could be redirected between calls in some testing scenarios.
- The WASM stub (`tty_wasm.go`) always returns `false` so that components built for the browser never attempt to use ANSI escape codes.
- Prefer this package over calling `term.IsTerminal` directly to keep the TTY detection logic centralized and easily testable.
- Components that need to adapt output for terminals (spinners, progress bars, colored messages) should call `IsStderrTerminal()` rather than checking `os.Stderr` directly.
- Components that request interactive input should call `IsStdinTerminal()` before reading from `os.Stdin`.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
