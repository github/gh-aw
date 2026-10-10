# scanfindings Package

The `scanfindings` package provides a shared representation of the findings reported by the scanner integrations (zizmor, poutine, grype, grant, runner-guard, yamllint, markdown security scan, audit findings).

## Overview

Each scanner speaks its own native JSON dialect, with severities spelled in a different vocabulary (`High`, `error`, `Negligible`, `note`, ...) and locations shaped differently. Integrations decode their native output into their own structs and then map those structs onto the shared `Finding` type declared here, so severity classification, ordering and rendering are implemented once instead of once per tool.

## Public API

### Types

| Type | Description |
|------|-------------|
| `SeverityLevel` | Shared severity vocabulary: `unknown`, `info`, `low`, `medium`, `high`, `critical` |
| `Finding` | Tool-independent finding: `RuleID`, `Severity`, `Message`, `File`, `Line`, `Column`, `Context` |

### Functions and methods

| Function | Signature | Description |
|----------|-----------|-------------|
| `ParseSeverity` | `func ParseSeverity(raw string) SeverityLevel` | Normalizes a native severity label (case-insensitive) |
| `SeverityLevel.String` | `func (s SeverityLevel) String() string` | Canonical lowercase severity name |
| `SeverityLevel.Rank` | `func (s SeverityLevel) Rank() int` | Relative ordering, higher is more severe |
| `SeverityLevel.AtLeast` | `func (s SeverityLevel) AtLeast(min SeverityLevel) bool` | Severity threshold comparison |
| `SeverityLevel.ErrorType` | `func (s SeverityLevel) ErrorType() string` | Console error type (`error`, `warning`, `info`) |
| `Finding.CompilerError` | `func (f Finding) CompilerError() console.CompilerError` | Converts a finding to the console error format |
| `FormatMessage` | `func FormatMessage(severityLabel, ruleID, description string) string` | Builds the `[severity] rule: description` message |
| `Render` | `func Render(w io.Writer, findings []Finding)` | Writes findings with stderr-aware diagnostic styling |
| `Sort` | `func Sort(findings []Finding)` | Orders findings by file, line, column, severity, rule |
| `CountAtLeast` | `func CountAtLeast(findings []Finding, min SeverityLevel) int` | Counts findings at or above a severity |
| `ContextLines` | `func ContextLines(fileLines []string, line int) []string` | Returns the source lines surrounding a finding |

## Usage Examples

`Render` uses stderr terminal/color detection for scanner diagnostics, even when
`w` is a buffer for testing. It does not redirect writes: pass `os.Stderr` for
the production diagnostic stream.

```go
import "github.com/github/gh-aw/pkg/scanfindings"

findings := []scanfindings.Finding{{
    RuleID:   "template-injection",
    Severity: scanfindings.ParseSeverity("High"),
    Message:  scanfindings.FormatMessage("High", "template-injection", "template injection with untrusted input"),
    File:     ".github/workflows/demo.lock.yml",
    Line:     12,
    Column:   24,
}}

scanfindings.Sort(findings)
scanfindings.Render(os.Stderr, findings)

highCount := scanfindings.CountAtLeast(findings, scanfindings.SeverityHigh)
```

## Dependencies

**Internal**:
- `pkg/console` — console error formatting

**External**:
- None beyond the Go standard library.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
