# jsonutil Package

> Compact JSON serialization utilities for safe expression handling and complete-document decoding.

## Overview

The `jsonutil` package provides a thin wrapper around Go's standard `encoding/json` encoder to produce compact JSON output without HTML escaping. This is essential for serializing GitHub Actions expressions (e.g. `${{ env.X && env.Y }}`) that contain characters such as `&`, `<`, and `>` which the standard `json.Marshal` would otherwise encode as `\u0026`, `\u003c`, and `\u003e`.

It also decodes JSON strings without copying the input into a byte slice. Decoding rejects malformed input and additional trailing values while allowing trailing whitespace.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `MarshalCompactNoHTMLEscape` | `func(v any) (string, error)` | Marshals `v` to compact JSON without HTML escaping, trimming the trailing newline emitted by `json.Encoder` |
| `UnmarshalString` | `func(input string, target any) error` | Decodes one complete JSON value without copying the string; rejects malformed input and trailing values |

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/jsonutil"

data := map[string]string{
    "expr": "${{ env.MCP_ENV == 'staging' && env.MCP_URL_STAGING || env.MCP_URL_PROD }}",
}

result, err := jsonutil.MarshalCompactNoHTMLEscape(data)
if err != nil {
    return fmt.Errorf("failed to marshal: %w", err)
}
// result: {"expr":"${{ env.MCP_ENV == 'staging' && env.MCP_URL_STAGING || env.MCP_URL_PROD }}"}
// Note: '&&' and '||' are preserved — not escaped to \u0026\u0026 or \u007c\u007c
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — package-scoped logger used for marshal error diagnostics

## Design Notes

- Uses `json.Encoder` with `SetEscapeHTML(false)` rather than `json.Marshal` to suppress HTML escaping.
- `SetEscapeHTML` is a method on the standard-library `json.Encoder`, not an exported function in `jsonutil`.
- The trailing newline that `json.Encoder.Encode` appends is trimmed with `strings.TrimSuffix` so the result is consistent with `json.Marshal` output.
- The package has no external dependencies beyond the Go standard library.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
