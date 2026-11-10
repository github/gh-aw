# actions Package

> Native GitHub Actions workflow AST and source-preserving YAML transformations.

## Overview

`actions` models GitHub Actions workflow properties, jobs, steps, expressions, and output references. It does not model agentic workflow frontmatter; that source AST is provided by [`aw`](../aw/README.md).

The resolver validates output references and job dependencies without changing the input. Lowering builds a deterministic YAML value tree, and emission preserves original comments, field order, quoting, and line endings where possible. Authored expression strings remain opaque; this package does not evaluate them.

## Public API

| Symbol | Description |
|--------|-------------|
| `Workflow`, `Job`, `Step` | Native workflow, job, and step models. |
| `RunStep`, `ActionStep`, `RawStep` | Typed and raw representations of workflow steps. |
| `OutputDecl`, `OutputRef` | Producer declarations and references used to resolve step and job outputs. |
| `Parse` | Parses and validates native workflow YAML. |
| `Resolve` | Validates the dependency graph and derives output bindings and `needs` edges. |
| `Lower`, `LowerStep` | Converts the resolved AST to YAML-compatible values. |
| `Emit` | Serializes a workflow, retaining source formatting and comments for parsed documents. |
| `EditSource` | Merges semantic changes into authored YAML while retaining unaffected syntax. |
| `ValidateJob` | Validates one job's native Actions structure. |

Workflow and step IDs are job-scoped. A step can read only outputs from earlier steps in its job, and workflow-level environment values cannot reference job or step outputs.

## Related documentation

The parent [`workflow` package specification](../README.md) documents compiler integration and the other workflow subsystems.
