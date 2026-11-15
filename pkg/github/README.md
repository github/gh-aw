# github Package

The `github` package provides label-based objective value mapping for issue prioritization scoring.

## Overview

This package defines how GitHub issue labels are translated into numeric objective values. It supports configurable mapping strategies (max, sum, first) and can load its configuration from an environment variable, a repository config file, or built-in defaults.

## Public API

### Types

| Type | Description |
|------|-------------|
| `ObjectiveMapping` | Defines how GitHub labels map to numeric objective values, including the combination logic for multiple matching labels |

#### `ObjectiveMapping` Fields

| Field | Type | Description |
|-------|------|-------------|
| `LabelToValue` | `map[string]int` | Maps label names (case-insensitive) to numeric values |
| `MultiLabelLogic` | `string` | How multiple matching labels are combined: `"max"` (default), `"sum"`, or `"first"` |
| `PriorityLabels` | `[]string` | Evaluation order when `MultiLabelLogic` is `"first"` |

### Methods on `*ObjectiveMapping`

| Method | Signature | Description |
|--------|-----------|-------------|
| `ComputeObjectiveValue` | `func(issueLabels []string) int` | Calculates the numeric value for an issue based on its labels; returns `0` if no labels match or if the receiver is `nil` |
| `FilterObjectiveLabels` | `func(issueLabels []string) []string` | Returns the subset of `issueLabels` that have defined objective values, preserving original order |
| `HasObjectiveLabel` | `func(label string) bool` | Reports whether a given label has a defined objective value |
| `GetAllLabels` | `func() []string` | Returns all labels defined in the mapping, sorted alphabetically |
| `MarshalJSON` | `func() ([]byte, error)` | Implements `json.Marshaler`; produces indented JSON output |
| `String` | `func() string` | Returns a human-readable summary: `ObjectiveMapping{labels: N, logic: X, priorities: M}` |

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `DefaultObjectiveMapping` | `func() *ObjectiveMapping` | Returns the built-in default label-to-value mapping |
| `LoadObjectiveMapping` | `func() *ObjectiveMapping` | Loads the mapping from environment, config file, or defaults (see precedence below) |

### Constants

The package exports constants for multi-label logic options:

| Constant | Value | Description |
|----------|-------|-------------|
| `MultiLabelLogicMax` | `"max"` | Use the highest matching label value (default) |
| `MultiLabelLogicSum` | `"sum"` | Sum all matching label values |
| `MultiLabelLogicFirst` | `"first"` | Use the first match in priority order |

For backward compatibility, deprecated exported `ObjectiveLabel*` and
`ObjectiveValue*` constants are still available for external consumers. Runtime
scoring behavior is not derived from those constants.

The deprecated label constants name the historical issue labels; the paired
value constants expose their legacy numeric values. They are aliases for
compatibility only and do not configure `DefaultObjectiveMapping()`:

`ObjectiveLabelAIGenerated`, `ObjectiveLabelAIInspected`, `ObjectiveLabelActions`,
`ObjectiveLabelBug`, `ObjectiveLabelCLI`, `ObjectiveLabelCopilotOpt`,
`ObjectiveLabelCritical`, `ObjectiveLabelDependencies`,
`ObjectiveLabelDocumentation`, `ObjectiveLabelEngine`,
`ObjectiveLabelEnhancement`, `ObjectiveLabelGoodFirstIssue`,
`ObjectiveLabelHighPriority`, `ObjectiveLabelLintMonster`,
`ObjectiveLabelLowPriority`, `ObjectiveLabelMCP`, `ObjectiveLabelMediumPriority`,
`ObjectiveLabelP0`, `ObjectiveLabelP1`, `ObjectiveLabelP2`, `ObjectiveLabelP3`,
`ObjectiveLabelPerformance`, `ObjectiveLabelQuestion`, `ObjectiveLabelReliability`,
`ObjectiveLabelSecurityFix`, `ObjectiveLabelSmokeCopilot`, `ObjectiveLabelTesting`,
and `ObjectiveLabelWorkflow`; and their corresponding value constants:
`ObjectiveValueAIGenerated`, `ObjectiveValueAIInspected`, `ObjectiveValueActions`,
`ObjectiveValueBug`, `ObjectiveValueCLI`, `ObjectiveValueCopilotOpt`,
`ObjectiveValueCritical`, `ObjectiveValueDependencies`,
`ObjectiveValueDocumentation`, `ObjectiveValueEngine`,
`ObjectiveValueEnhancement`, `ObjectiveValueGoodFirstIssue`,
`ObjectiveValueHighPriority`, `ObjectiveValueLintMonster`,
`ObjectiveValueLowPriority`, `ObjectiveValueMCP`, `ObjectiveValueMediumPriority`,
`ObjectiveValueP0`, `ObjectiveValueP1`, `ObjectiveValueP2`, `ObjectiveValueP3`,
`ObjectiveValuePerformance`, `ObjectiveValueQuestion`, `ObjectiveValueReliability`,
`ObjectiveValueSecurityFix`, `ObjectiveValueSmokeCopilot`, `ObjectiveValueTesting`,
and `ObjectiveValueWorkflow`.

Default label-to-value entries are provided by `DefaultObjectiveMapping()`:

| Label | Value |
|-------|-------|
| `critical` | 100 |
| `p0` | 100 |
| `security-fix` | 75 |
| `high-priority` | 50 |
| `copilot-opt` | 50 |
| `p1` | 50 |
| `performance` | 30 |
| `medium-priority` | 25 |
| `p2` | 25 |
| `low-priority` | 10 |
| `p3` | 10 |
| `documentation` | 5 |

Labels not listed above (for example `bug`, `testing`, and `reliability`) are
not part of the default mapping and evaluate to `0` unless provided by
configuration.

## Configuration Precedence

`LoadObjectiveMapping` resolves the mapping in this order:

1. **`OBJECTIVE_MAPPING_JSON` environment variable** — interpreted first as a raw JSON string; if parsing fails, treated as a file path from which JSON is read.
2. **`.github/objective-mapping.json`** — a repository-level override file.
3. **Built-in defaults** — returned by `DefaultObjectiveMapping()`.

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/github"

// Load mapping (env > config file > defaults)
om := github.LoadObjectiveMapping()

// Score an issue by its labels (default mapping: critical=100, high-priority=50, p1=50, ...)
score := om.ComputeObjectiveValue([]string{"critical", "high-priority"})
// score == 100  (max of critical=100, high-priority=50)

// Check which labels contributed
objectiveLabels := om.FilterObjectiveLabels([]string{"critical", "high-priority"})
// objectiveLabels == ["critical", "high-priority"]

// Use the default mapping directly
defaults := github.DefaultObjectiveMapping()
fmt.Println(defaults) // ObjectiveMapping{labels: 12, logic: max, priorities: 7}
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — debug logging via `logger.New("github:label_objective_mapping")`

**External**:
- None beyond the Go standard library (`encoding/json`, `fmt`, `os`, `path/filepath`, `slices`, `strings`).

## Design Notes

- All label comparisons are case-insensitive: labels are normalised with `strings.ToLower(strings.TrimSpace(...))` before lookup.
- The default `MultiLabelLogic` is `"max"`. Callers that do not set this field get max-value semantics automatically.
- `PriorityLabels` is only consulted when `MultiLabelLogic` is `"first"`; the implementation walks the issue's labels in their existing order and returns the value for the first label that also appears in `PriorityLabels`. If no priority entry matches, it falls back to the first matched value collected from `LabelToValue`.
- Debug output is controlled by the `DEBUG=github:*` environment variable and is only emitted when that variable is set.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
