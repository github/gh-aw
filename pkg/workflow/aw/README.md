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
| `Parse` | Parses authored Markdown into a `Document`. |
| `Emit` | Emits a `Document`, retaining unchanged source and deterministically serializing edits. |
| `ExecutionBlocks` | Extracts execution blocks from an authored document for workflow analysis. |
| `AuthoredJobDependencies` | Builds the dependency graph for authored custom jobs. |
| `ValidateStatusOnlyJob` | Validates status-only job requirements and returns structured violations. |

`workflow.Compiler.ParseWorkflowDocument` accepts this AST as input to compilation.

## Dependencies

The package uses the shared frontmatter parser and does not resolve imports or validate workflow schemas itself.
