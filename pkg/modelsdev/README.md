# modelsdev Package

The `modelsdev` package provides helpers for normalizing model provider and model identifiers.

## Overview

Use these helpers to normalize provider aliases and model identifiers for consistent comparisons.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NormalizeProvider` | `func(provider string) string` | Normalizes provider aliases such as `github`, `copilot`, and `github_models` to `github-copilot`; other identifiers are trimmed and lower-cased |
| `NormalizeComparableModelID` | `func(value string) string` | Lower-cases a model identifier, trims surrounding whitespace, and replaces `.` and `_` with `-` so equivalent model IDs compare consistently |

## Usage Example

```go
import "github.com/github/gh-aw/pkg/modelsdev"

provider := modelsdev.NormalizeProvider("github")          // "github-copilot"
modelID := modelsdev.NormalizeComparableModelID("GPT_4.1") // "gpt-4-1"
_ = provider
_ = modelID
```

## Design Notes

- Provider aliases such as `github`, `copilot`, and `github_models` are normalized to `github-copilot`.
- Comparable model matching normalizes separators (`.` and `_` to `-`) to improve lookup robustness.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
