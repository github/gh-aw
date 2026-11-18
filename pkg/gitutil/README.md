# gitutil Package

> Utility functions for Git repository operations and SHA/ref validation.

## Overview

The `gitutil` package contains helpers for:
- Validating hex strings (e.g. commit SHAs).
- Extracting base repository slugs from action paths.
- Finding the root directory of the current Git repository using pure Go filesystem traversal.
- Reading file contents from the `HEAD` commit via a `git` subprocess.
- Running isolated Git commands for disposable source downloads.

## Public API

### Variables

| Variable | Type | Description |
|----------|------|-------------|
| `ErrNotGitRepository` | `error` | Sentinel error returned by `FindGitRoot` and `FindGitRootFrom` when no `.git` entry is found while traversing up to the filesystem root |

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `IsHexString` | `func(s string) bool` | Returns `true` if `s` consists entirely of hexadecimal characters (`0–9`, `a–f`, `A–F`); returns `false` for the empty string |
| `IsValidFullSHA` | `func(s string) bool` | Returns `true` if `s` is a valid 40-character lowercase hexadecimal SHA (matches `^[0-9a-f]{40}$`) |
| `IsValidFullSHACaseInsensitive` | `func(s string) bool` | Returns `true` if `s` is a valid 40-character hexadecimal SHA with either uppercase or lowercase letters |
| `ValidateGitRef` | `func(ref string) error` | Returns an error if `ref` would be unsafe to pass as a positional argument to a `git` subprocess: rejects empty refs, refs starting with `-` (argument injection, CWE-88), refs containing NUL bytes, and refs containing `..` (object traversal expressions) |
| `ValidateGitPath` | `func(path string) error` | Returns an error if `path` would be unsafe to pass as a positional argument to a `git` subprocess: rejects empty paths, paths starting with `-` (argument injection, CWE-88), absolute paths, and paths that resolve (after `path.Clean`) to `..` or contain a leading `../` traversal segment |
| `ExtractBaseRepo` | `func(repoPath string) string` | Extracts the `owner/repo` portion from an action path that may include a sub-folder (e.g. `github/codeql-action/upload-sarif` → `github/codeql-action`) |
| `Getwd` | `func() (string, error)` | Returns the current working directory (like `os.Getwd()`), wrapping any error with actionable recovery guidance |
| `UserHomeDir` | `func() (string, error)` | Returns the current user's home directory (like `os.UserHomeDir()`), wrapping any error with actionable recovery guidance |
| `FindGitRoot` | `func() (string, error)` | Returns the absolute path of the root directory of the current Git repository using pure Go filesystem traversal (no `git` subprocess); starts from the current working directory |
| `FindGitRootFrom` | `func(startDir string) (string, error)` | Like `FindGitRoot` but starts from `startDir`; traverses upward looking for a `.git` directory or worktree marker file |
| `ReadFileFromHEAD` | `func(filePath, gitRoot string) (string, error)` | Reads a file's content from the `HEAD` commit without `git show HEAD:path` interpolation by resolving a literal tree entry with `git ls-tree` and then reading the blob with `git cat-file`; rejects paths that escape the repository; requires `git` on `PATH` |
| `SourceCommandContext` | `func(ctx context.Context, remoteURL string, args ...string) *exec.Cmd` | Constructs an isolated Git command for disposable source downloads and their follow-up operations; allows only HTTPS transport, disables hooks, templates, fsmonitor, credential helpers, interactive prompts and automatic maintenance, and ignores inherited Git configuration and environment overrides |
| `SourceCommandOutputContext` | `func(ctx context.Context, remoteURL string, args ...string) ([]byte, error)` | Runs an isolated Git command capturing stdout, retrying anonymously if the configured token is rejected |
| `SourceCommandCombinedOutputContext` | `func(ctx context.Context, remoteURL string, args ...string) ([]byte, error)` | Runs an isolated Git command capturing combined output, retrying anonymously if the configured token is rejected |
| `CreateSourceTempDir` | `func(pattern string) (string, error)` | Creates an absolute temporary directory for source-download clones |

**Behavioral contracts**:

- `IsHexString` MUST return `false` for the empty string.
- `IsValidFullSHA` MUST require exactly 40 lowercase hexadecimal characters; mixed-case or shorter strings MUST return `false`.
- `IsValidFullSHACaseInsensitive` MUST require exactly 40 hexadecimal characters and accept uppercase and lowercase letters.
- `ValidateGitRef` MUST return an error for an empty ref, a ref starting with `-`, a ref containing a NUL byte, or a ref containing `..`.
- `ValidateGitPath` MUST return an error for an empty path, a path starting with `-`, an absolute path, or a path that is `..` or starts with `../` after `path.Clean`.
- `FindGitRoot` and `FindGitRootFrom` MUST return `ErrNotGitRepository` (not a wrapped error) when the filesystem root is reached without finding a `.git` entry.
- `FindGitRootFrom` MUST accept both `.git` directories (normal repositories) and `.git` files whose content begins with `gitdir:` (worktrees and submodules).
- `Getwd` and `UserHomeDir` MUST wrap the underlying `os` error with actionable recovery guidance rather than returning a bare `fmt.Errorf("...: %w", err)`.
- `ReadFileFromHEAD` MUST return an error when `gitRoot` is empty.
- `ReadFileFromHEAD` MUST return an error when `filePath` resolves to a path outside `gitRoot`.
- Source-download clones and subsequent Git commands MUST use the isolated source-command helpers, not inherit development-checkout settings. Commands run outside any discovered checkout and bound repository discovery at that working directory; explicit `-C` operations on the temporary clone remain supported. Global/system filters, local checkout configuration and URL rewrites are ignored. HTTPS redirects are followed only for the initial request. The HTTPS helper path is derived from the selected Git installation, not inherited from `GIT_EXEC_PATH`. Authentication uses host-scoped `gh` tokens passed only through the child environment, never command arguments or repository configuration; authentication rejection triggers an anonymous retry for public sources. SSH, local-file transports, custom remote helpers and interactive credential helpers are intentionally unsupported on these download paths; development checkouts are unchanged.

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/gitutil"

// Validate a commit SHA
if gitutil.IsValidFullSHA(commitSHA) {
    fmt.Println("Valid 40-character commit SHA")
}

// Validate a ref before passing it to a git subprocess
if err := gitutil.ValidateGitRef(userSuppliedRef); err != nil {
    return fmt.Errorf("unsafe git ref: %w", err)
}

// Validate a path before passing it to a git subprocess
if err := gitutil.ValidateGitPath(userSuppliedPath); err != nil {
    return fmt.Errorf("unsafe git path: %w", err)
}

// Find the git repository root (pure Go, no git subprocess)
root, err := gitutil.FindGitRoot()
if errors.Is(err, gitutil.ErrNotGitRepository) {
    return err
} else if err != nil {
    return err
}

// Find the git root starting from a specific directory
root, err = gitutil.FindGitRootFrom("/some/subdir")
if err != nil {
    return fmt.Errorf("not in a git repository: %w", err)
}

// Read a file from the HEAD commit (prefer absolute paths under root)
content, err := gitutil.ReadFileFromHEAD(filepath.Join(root, "go.mod"), root)
```

## Thread Safety

All exported functions are safe for concurrent use. The SHA-validation functions (`IsHexString`, `IsValidFullSHA`) are pure functions with no shared state. `FindGitRoot` and `FindGitRootFrom` read only the filesystem and the process working directory. `ReadFileFromHEAD` spawns a `git` subprocess per call with no shared state.

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — debug logging

**Go packages**:
- `github.com/cli/go-gh/v2/pkg/auth` — host-scoped credentials for source downloads

**External** (runtime):
- `git` executable on `PATH` — required by `ReadFileFromHEAD` and `SourceCommandContext`

## Design Decisions

- `FindGitRoot` and `FindGitRootFrom` use pure Go filesystem traversal (walking up the directory tree looking for `.git`), avoiding the need for a `git` executable on `PATH`. This is important for Rosetta 2 compatibility on macOS ARM64 and restricted environments where `git` may not be available.
- `FindGitRootFrom` verifies worktree marker file content (must begin with `gitdir:`) in addition to existence, guarding against false positives from unrelated files named `.git`.
- `ReadFileFromHEAD` requires `git` on `PATH` because resolving a literal tree entry and reading the resulting blob with `git ls-tree`/`git cat-file` avoids `git show HEAD:path` interpolation and is more reliable than re-implementing pack-file parsing in pure Go.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
