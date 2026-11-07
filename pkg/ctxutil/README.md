# ctxutil Package

> Small helpers for consistent `context.Context` handling.

## Overview

The `ctxutil` package centralizes context fallback conventions used across the repository. It currently exposes a single helper, `OrBackground`, for call sites that accept an optional context and need a non-nil `context.Context` before invoking context-aware APIs.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `OrBackground` | `func(ctx context.Context) context.Context` | Returns `context.Background()` when `ctx` is nil; otherwise returns `ctx` unchanged |
| `StartOperation` | `func(ctx context.Context, operation string) func(error)` | Reports an operation start when the context has an observer and returns its completion callback. |
| `WithOperationReporter` | `func(ctx context.Context, report func(string) func(error)) context.Context` | Attaches an operation-start callback to a context; the callback returns a completion callback that receives the operation error. |

## Usage Examples

```go
ctx := ctxutil.OrBackground(ctx)
```

## Dependencies

**External**:
- None beyond the Go standard library (`context`).

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
