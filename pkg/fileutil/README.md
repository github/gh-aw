# fileutil Package

> Utility functions for security-conscious file path validation and common file operations.

## Overview

The `fileutil` package focuses on security-conscious file handling: path validation, directory-boundary enforcement, and straightforward file and directory operations. It also provides a cross-platform tar extraction helper that guards against path-traversal payloads embedded in archives.

All functions emit debug output only when `DEBUG=fileutil:*` is active, and never write to stdout.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ValidateAbsolutePath` | `func(path string) (string, error)` | Validates that a file path is absolute and safe; rejects empty paths, cleans with `filepath.Clean`, and verifies the result is absolute |
| `ValidatePathWithinBase` | `func(base, candidate string) error` | Checks that `candidate` is located within the `base` directory tree; resolves symlinks before comparison to prevent traversal and symlink escapes |
| `FileExists` | `func(path string) bool` | Returns `true` if `path` exists and is a regular file (not a directory) |
| `DirExists` | `func(path string) bool` | Returns `true` if `path` exists and is a directory |
| `IsDirEmpty` | `func(path string) bool` | Returns `true` if the directory at `path` contains no entries; also returns `true` if the directory cannot be read |
| `CopyFile` | `func(src, dst string) error` | Copies the file at `src` to `dst` using buffered I/O; calls `Sync` on the destination before closing; removes the partial destination file on write error |
| `EnsureParentDir` | `func(path string, perm os.FileMode) error` | Ensures the parent directory of `path` exists, creating it recursively with the given permissions; returns an error for empty paths |
| `ExtractFileFromTar` | `func(data []byte, path string) ([]byte, error)` | Extracts a single file by `path` from a tar archive; rejects unsafe entry names (absolute or `..`-containing paths) using `filepath.IsLocal` |
| `ValidateExecutablePath` | `func(path string) (string, error)` | Validates that `path` is absolute, resolves symlinks, points to a regular file, and (on non-Windows platforms) is executable; returns the resolved, cleaned path |
| `ResolveExecutablePath` | `func(name string) (string, error)` | Resolves `name` from `PATH` via `exec.LookPath`, makes the result absolute if necessary, and validates it with `ValidateExecutablePath` |
| `ReadFileWithinBase` | `func(base, candidate string) (content []byte, err error)` | Reads an existing file while preventing symlinks from escaping `base`; the base directory itself must not be a symlink. |

**Behavioral contracts**:

- `ValidateAbsolutePath` MUST reject empty paths and MUST return an error for any path that is not absolute after `filepath.Clean`.
- `ValidatePathWithinBase` MUST resolve symlinks via `filepath.EvalSymlinks` (falling back to `filepath.Abs` for non-existent paths) before comparing, so neither `..` components nor symlinks pointing outside `base` can escape the boundary.
- `IsDirEmpty` MUST return `true` when the directory cannot be read (treats unreadable as empty).
- `CopyFile` MUST call `Sync` on the destination before closing, and MUST remove the partial destination file if a write error occurs mid-copy.
- `ExtractFileFromTar` MUST reject the caller-supplied `path` and each tar entry name that does not satisfy `filepath.IsLocal`; returns an error when the requested file is not present in the archive.
- `ValidateExecutablePath` MUST return an error if `path` does not resolve to an existing regular file, or (on non-Windows platforms) if the file lacks any executable bit (`mode&0o111 == 0`).
- `ResolveExecutablePath` MUST return an error for an empty `name`, and MUST return the underlying `exec.LookPath` error unchanged when `name` is not found on `PATH`.

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/fileutil"

// Validate and clean a user-supplied path
cleanPath, err := fileutil.ValidateAbsolutePath(userInput)
if err != nil {
    return fmt.Errorf("invalid path: %w", err)
}

// Ensure output path stays within workspace
if err := fileutil.ValidatePathWithinBase("/workspace", outputPath); err != nil {
    return fmt.Errorf("output path escapes workspace: %w", err)
}

// Copy a file (partial destination is removed on error)
if err := fileutil.CopyFile("source.txt", "destination.txt"); err != nil {
    return fmt.Errorf("copy failed: %w", err)
}

// Extract a file from a tar archive in memory
content, err := fileutil.ExtractFileFromTar(tarBytes, "dist/binary")
if err != nil {
    return fmt.Errorf("extraction failed: %w", err)
}

// Validate a fully-qualified executable path
resolvedExe, err := fileutil.ValidateExecutablePath("/usr/local/bin/git")
if err != nil {
    return fmt.Errorf("invalid executable: %w", err)
}

// Resolve and validate an executable by name from PATH
gitPath, err := fileutil.ResolveExecutablePath("git")
if err != nil {
    return fmt.Errorf("git not found: %w", err)
}
```

## Thread Safety

All exported functions are safe for concurrent use. None of them share mutable package-level state; the package-level logger variables (`fileutilLog`, `tarLog`, `executableLog`) are read-only after package initialization.

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — debug logging

## Design Decisions

- `ValidatePathWithinBase` resolves symlinks before comparison, providing defence-in-depth against symlink attacks in addition to the `..` checking that `ValidateAbsolutePath` provides.
- `ExtractFileFromTar` uses Go's standard `archive/tar` instead of an external `tar` process, ensuring cross-platform compatibility in environments where `tar` may not be on `PATH`.
- `CopyFile` removes the partial destination file on write error to prevent leaving corrupt files behind.
- `ValidateExecutablePath` resolves symlinks and checks the executable bit (skipped on Windows, which has no POSIX permission bits) to defend against invoking non-executable or unexpected files.
- `ResolveExecutablePath` layers `ValidateExecutablePath` on top of `exec.LookPath` so callers get the same security guarantees whether they already have a full path or only a command name.
- All debug output uses the `logger` package and is only emitted when `DEBUG=fileutil:*`.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
