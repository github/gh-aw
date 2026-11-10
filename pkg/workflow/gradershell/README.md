# gradershell Package

> Loading repository Bash graders and checking a bounded set of direct file-write patterns.

## Public API

| Symbol | Description |
|--------|-------------|
| `ValidRunPath` | Accepts literal relative `.sh` paths without traversal or shell expressions. |
| `Load` | Reads a regular UTF-8 Bash source file from within the repository, without executing it. |
| `Check` | Returns findings for supported direct file-write patterns in a Bash source string. |
| `Finding` | Structured result containing a source line and diagnostic message. |

The loader rejects symlinks, files larger than 64 KiB, invalid UTF-8, NUL bytes, and files without a Bash shebang. `Check` is a finite static check, not a sandbox or proof that arbitrary shell scripts are safe to execute; only trusted repository scripts should run until execution is isolated.
