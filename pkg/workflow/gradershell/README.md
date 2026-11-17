# gradershell Package

> Shared Bash AST helpers for run-step sanitization.

## Public API

| Symbol | Description |
|--------|-------------|
| `HasShellCodeEvaluation` | Identifies shell nodes that may reinterpret expanded values as code. |
| `LiteralWord` | Extracts a statically literal shell word when one is available. |

The compiler uses these helpers to decide when authored run-step expressions
must be extracted into environment variables instead of remaining inline.
