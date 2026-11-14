# aw Package

> Source AST for agentic workflow Markdown documents.

## Overview

`aw` parses the authored Markdown document into YAML frontmatter and prompt text while retaining source locations. Parsing and emission preserve unchanged source bytes, including comments, line endings, and prompt whitespace. This AST models agentic workflow configuration; it is separate from the native GitHub Actions AST in [`actions`](../actions/README.md).

Schema validation, import resolution, and typed workflow configuration remain the responsibility of the parent `workflow` compiler.

## Public API

| Symbol | Description |
|--------|-------------|
| `Document` | Frontmatter, Markdown body, and source locations for one authored workflow. |
| `SourceLocation` | Frontmatter lines and field locations used by diagnostics and source edits. |
| `ExecutionBlock`, `ExecutionStep` | Read-only views of authored execution scopes and their steps, with source paths and fields. |
| `JobDependencyGraph`, `StatusOnlyViolation` | Transitive job dependency graph and structured status-only validation diagnostic. |
| `Parse` | Parses authored Markdown into a `Document`. |
| `Emit` | Emits a `Document`, retaining unchanged source and deterministically serializing edits. |
| `ParseActionsExpression` | Parses exactly one nonempty Actions expression into its syntax tree without semantic validation. |
| `ExecutionBlocks` | Extracts execution blocks from an authored document for workflow analysis. |
| `AuthoredJobDependencies` | Builds the dependency graph for authored custom jobs. |
| `JobDependencyGraph.DependsOn` | Reports whether a job depends directly or transitively on another job; safe to call before cycle validation. |
| `JobDependencyGraph.IsPreAgentCustomJob` | Reports whether graph edges prove a custom job runs before agent without depending on agent or detection data. |
| `ExecutionBlock.FieldPath` | Joins a field name to the execution block's source path. |
| `IsStatusOnlyJob` | Reports whether a custom job opts to continue after detection errors. |
| `HasSuccessfulDetectionCondition` | Reports whether a job condition requires detection success on every branch; an empty condition is considered successful. |
| `ValidateStatusOnlyJob` | Validates status-only job access to agent data and returns structured violations. |

`workflow.Compiler.ParseWorkflowDocument` accepts this AST as input to compilation.

## Dependencies

The package uses the shared frontmatter parser and does not resolve imports or validate workflow schemas itself.
