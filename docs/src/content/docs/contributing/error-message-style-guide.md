---
title: Error Messages
description: Write actionable, constructive error messages with examples.
---

# Error message style guide

Use actionable messages that explain what went wrong, what is expected, and how to fix it.

## Prefer constructive language

Avoid bare words like `invalid`, `cannot`, `must`, or `failed` when they do not explain the fix. Prefer messages that include what was expected and, when helpful, an example.

✅ `invalid repo format 'gh-aw' — expected 'owner/repo' format (for example: 'github/gh-aw')`

❌ `invalid repo format`

## When to use `NewValidationError` vs `fmt.Errorf`

Use `NewValidationError(field, value, reason, suggestion)` in validation code (`*_validation.go`) so users get a structured reason and suggestion.

Use `fmt.Errorf` for operational wrapping (`%w`) outside validation logic when you include specific context and recovery guidance.

## Error type selection

Use `NewValidationError(...)` for bad input or config shape, missing fields, and unsupported values. Use `NewOperationError(...)` for runtime failures such as fetching, file IO, network, or command execution. Use `NewConfigurationError(...)` for safe-outputs and config wiring errors. Use `fmt.Errorf(...%w...)` to wrap lower-level errors with actionable context.

## Suggestion text requirements

Good suggestions say what to change, include a concrete YAML or code example, and use ✓/✗ examples when ambiguity is likely.

Example:

```text
Use a supported engine.
✓ Example:
engine: copilot

✗ Avoid:
engine: unknown
```

## YAML example guidance

Keep examples minimal and valid YAML, use real field names from frontmatter, and quote only when required by YAML syntax.
