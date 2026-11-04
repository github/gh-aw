# atif Package

> Converts unified gh-aw session JSONL into ATIF-v1.8 trajectories.

## Overview

The `atif` package reads one versioned unified-session JSONL stream and converts it to the emitted subset of the Agent Trajectory Interchange Format (ATIF) v1.8. The output preserves gh-aw provenance and session evidence in schema-approved extension fields while mapping conversation messages, tool calls, observations, and metrics into trajectory steps.

`Convert` validates and reconciles source records while reading, with a 64 MiB input limit. `Trajectory.Validate` checks the emitted ATIF subset, including references between tool calls and observations in the same step. `Encode` validates a trajectory before writing formatted JSON with a trailing newline.

## Public API

### Types

| Type | Kind | Description |
|------|------|-------------|
| `Trajectory` | struct | ATIF document containing session metadata, steps, and final metrics. |
| `Agent` | struct | Agent identity and metadata in the trajectory. |
| `Step` | struct | One ordered trajectory step containing messages and tool activity. |
| `ToolCall` | struct | Tool invocation recorded in a step. |
| `Observation` | struct | Tool result or other observation associated with a step. |
| `ObservationResult` | struct | Structured observation content and status. |
| `FinalMetrics` | struct | Aggregated session metrics attached to the trajectory. |

### Functions and Methods

| Function | Signature | Description |
|----------|-----------|-------------|
| `Convert` | `func Convert(ctx context.Context, input io.Reader) (*Trajectory, error)` | Converts one unified-session JSONL stream into a trajectory. |
| `(*Trajectory).Validate` | `func (t *Trajectory) Validate() error` | Validates the emitted ATIF subset and same-step tool references. |
| `Encode` | `func Encode(w io.Writer, trajectory *Trajectory) error` | Validates and writes formatted trajectory JSON followed by a newline. |

### Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `SchemaVersion` | untyped string | `"ATIF-v1.8"` | Schema version written to trajectories. |
| `MaxSessionBytes` | untyped integer | `64 * 1024 * 1024` | Maximum input size accepted by `Convert`. |

## Usage Example

```go
input, err := os.Open("session.jsonl")
if err != nil {
	panic(err)
}
defer input.Close()

trajectory, err := atif.Convert(ctx, input)
if err != nil {
	panic(err)
}

if err := atif.Encode(os.Stdout, trajectory); err != nil {
	panic(err)
}
```

## Design Decisions

Conversion is bounded and streaming: it reads from an `io.Reader` rather than requiring the full source file in memory. Tool correlation and metric reconciliation retain the source scope, so unrelated sessions or components are not combined. Invalid input or an invalid trajectory is returned as an error instead of being silently emitted.

The package emits a constrained ATIF subset rather than attempting to preserve arbitrary source data as first-class ATIF fields. Additional gh-aw evidence is carried in schema-approved extras.

## Dependencies

Production code uses the Go standard library and `pkg/logger` for debug logging. The compatibility fixtures in `testdata/` are generated with `Convert` and `Encode`; validating them with Harbor is an optional development check, not a package dependency.

## Thread Safety

The converter and encoder do not retain package-level mutable session state. Callers remain responsible for synchronization when sharing or mutating a `Trajectory`.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
